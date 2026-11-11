package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	adapter "encore.app/notification-adapters/aliyunsms"
	"encore.app/notification-adapters/sdk"
)

const (
	defaultPluginListen   = "127.0.0.1:5004"
	defaultInternalListen = "127.0.0.1:5005"
	defaultPollInterval   = 2 * time.Second
	workerShutdownTimeout = 5 * time.Second
	serverShutdownTimeout = 10 * time.Second
	maxRoutesConfigBytes  = 64 << 10
	maxReceiptTokenBytes  = 4096
)

type appConfig struct {
	pluginToken    string
	pluginListen   string
	internalListen string
	uiOrigins      []string
	reportWorker   bool
	ledgerPath     string
	ledgerKey      []byte
	mns            adapter.MNSConfig
	pollInterval   time.Duration
	routes         adapter.ReceiptRouteResolver
}

type runtimeComponents struct {
	provider *adapter.Provider
	worker   reportWorker
	ledger   ledgerCloser
}

type reportWorker interface {
	RunOnce(context.Context) error
}

type ledgerCloser interface {
	Close() error
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	config, err := loadAppConfig(os.Getenv)
	if err != nil {
		log.Fatal("invalid Alibaba SMS plugin configuration")
	}
	components, err := prepareRuntime(config)
	if err != nil {
		log.Fatal("could not prepare Alibaba SMS runtime")
	}
	pluginListener, err := net.Listen("tcp", config.pluginListen)
	if err != nil {
		closeLedger(components.ledger)
		log.Fatal("could not bind notification plugin listener")
	}
	internalListener, err := net.Listen("tcp", config.internalListen)
	if err != nil {
		_ = pluginListener.Close()
		closeLedger(components.ledger)
		log.Fatal("could not bind private readiness listener")
	}
	if err := runServers(ctx, config, components, pluginListener, internalListener); err != nil {
		// runServers retains the ledger lock if a worker or request fails to
		// drain. main exits immediately after this message.
		log.Fatal("Alibaba SMS plugin shutdown did not drain safely")
	}
}

func loadAppConfig(getenv func(string) string) (appConfig, error) {
	if getenv == nil {
		return appConfig{}, errors.New("environment lookup is required")
	}
	config := appConfig{
		pluginToken:    getenv("NOTIFICATION_PLUGIN_TOKEN"),
		pluginListen:   envOr(getenv, "NOTIFICATION_PLUGIN_LISTEN", defaultPluginListen),
		internalListen: envOr(getenv, "ALIYUN_SMS_INTERNAL_LISTEN", defaultInternalListen),
		pollInterval:   defaultPollInterval,
	}
	keepKey := false
	defer func() {
		if !keepKey {
			clear(config.ledgerKey)
		}
	}()
	if config.pluginToken == "" || !privateLoopbackAddress(config.internalListen) {
		return appConfig{}, errors.New("invalid plugin token or private readiness listener")
	}
	if value := getenv("NOTIFICATION_PLUGIN_UI_ORIGINS"); value != "" {
		config.uiOrigins = splitNonEmpty(value)
	}
	switch getenv("ALIYUN_SMS_REPORT_WORKER_ENABLED") {
	case "", "false":
		config.reportWorker = false
		keepKey = true
		return config, nil
	case "true":
		config.reportWorker = true
	default:
		return appConfig{}, errors.New("report worker switch must be true or false")
	}

	config.ledgerPath = strings.TrimSpace(getenv("ALIYUN_SMS_LEDGER_PATH"))
	keyPath := strings.TrimSpace(getenv("ALIYUN_SMS_LEDGER_KEY_FILE"))
	if config.ledgerPath == "" || keyPath == "" {
		return appConfig{}, errors.New("report worker requires a ledger path and key file")
	}
	key, err := readPrivateFile(keyPath, 32)
	if err != nil || len(key) != 32 {
		return appConfig{}, errors.New("report worker key file must be a private 32-byte regular file")
	}
	config.ledgerKey = key

	config.mns = adapter.MNSConfig{
		AccountID:       strings.TrimSpace(getenv("ALIYUN_SMS_ACCOUNT_ID")),
		Region:          strings.TrimSpace(getenv("ALIYUN_SMS_REGION")),
		QueueName:       strings.TrimSpace(getenv("ALIYUN_SMS_MNS_QUEUE_NAME")),
		AccessKeyID:     strings.TrimSpace(getenv("ALIYUN_SMS_MNS_ACCESS_KEY_ID")),
		AccessKeySecret: strings.TrimSpace(getenv("ALIYUN_SMS_MNS_ACCESS_KEY_SECRET")),
		WaitSeconds:     10,
	}
	if wait := getenv("ALIYUN_SMS_MNS_WAIT_SECONDS"); wait != "" {
		value, err := strconv.Atoi(wait)
		if err != nil || value < 1 || value > 30 {
			return appConfig{}, errors.New("invalid MNS long-poll duration")
		}
		config.mns.WaitSeconds = value
	}
	if !fixedMNSEndpoint(strings.TrimSpace(getenv("ALIYUN_SMS_MNS_ENDPOINT")), config.mns.AccountID, config.mns.Region) {
		return appConfig{}, errors.New("MNS endpoint is outside the fixed HTTPS allowlist")
	}
	if _, err := adapter.NewMNSQueue(config.mns); err != nil {
		return appConfig{}, errors.New("invalid MNS queue configuration")
	}

	if value := getenv("ALIYUN_SMS_REPORT_POLL_INTERVAL_SECONDS"); value != "" {
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 1 || seconds > 60 {
			return appConfig{}, errors.New("invalid report worker poll interval")
		}
		config.pollInterval = time.Duration(seconds) * time.Second
	}
	allowedOrigins := map[string]bool{}
	for _, origin := range splitNonEmpty(getenv("ALIYUN_SMS_CORE_ALLOWED_ORIGINS")) {
		if !validHTTPSOrigin(origin) {
			return appConfig{}, errors.New("core allowlist contains an invalid origin")
		}
		allowedOrigins[origin] = true
	}
	if len(allowedOrigins) == 0 {
		return appConfig{}, errors.New("report worker requires a non-empty core origin allowlist")
	}
	routes, err := readOperatorRoutes(strings.TrimSpace(getenv("ALIYUN_SMS_RECEIPT_ROUTES_FILE")), allowedOrigins)
	if err != nil {
		return appConfig{}, err
	}
	resolver, err := adapter.NewStaticReceiptRoutes(routes)
	if err != nil {
		return appConfig{}, errors.New("invalid fixed receipt route map")
	}
	config.routes = resolver
	keepKey = true
	return config, nil
}

func prepareRuntime(config appConfig) (runtimeComponents, error) {
	provider := adapter.New()
	if !config.reportWorker {
		return runtimeComponents{provider: provider}, nil
	}
	ledger, err := adapter.OpenSendLedger(config.ledgerPath, config.ledgerKey)
	clear(config.ledgerKey)
	if err != nil {
		return runtimeComponents{}, errors.New("could not open encrypted send ledger")
	}
	fail := func() (runtimeComponents, error) {
		_ = ledger.Close()
		return runtimeComponents{}, errors.New("could not prepare report worker")
	}
	provider, err = adapter.NewWithSendLedger(ledger)
	if err != nil {
		return fail()
	}
	queue, err := adapter.NewMNSQueue(config.mns)
	if err != nil {
		return fail()
	}
	worker, err := adapter.NewReportWorker(queue, ledger, config.routes, adapter.NewCoreReceiptPoster())
	if err != nil {
		return fail()
	}
	return runtimeComponents{provider: provider, worker: worker, ledger: ledger}, nil
}

func runServers(ctx context.Context, config appConfig, components runtimeComponents, pluginListener, internalListener net.Listener) error {
	if ctx == nil || components.provider == nil || pluginListener == nil || internalListener == nil || !privateLoopbackAddress(internalListener.Addr().String()) {
		closeListeners(pluginListener, internalListener)
		closeLedger(components.ledger)
		return errors.New("invalid listener configuration")
	}
	pluginHandler, err := sdk.NewHandler(config.pluginToken, components.provider, sdk.HandlerOptions{AllowedUIOrigins: config.uiOrigins})
	if err != nil {
		closeListeners(pluginListener, internalListener)
		closeLedger(components.ledger)
		return errors.New("invalid plugin handler configuration")
	}
	pluginMux := http.NewServeMux()
	pluginMux.Handle("/", pluginHandler)
	return runServersWithHandlers(ctx, config, components, pluginMux, pluginListener, internalListener, workerShutdownTimeout, serverShutdownTimeout)
}

func runServersWithHandlers(ctx context.Context, config appConfig, components runtimeComponents, pluginHandler http.Handler, pluginListener, internalListener net.Listener, workerTimeout, serverTimeout time.Duration) error {
	if ctx == nil || pluginHandler == nil || pluginListener == nil || internalListener == nil || !privateLoopbackAddress(internalListener.Addr().String()) {
		closeListeners(pluginListener, internalListener)
		closeLedger(components.ledger)
		return errors.New("invalid listener configuration")
	}
	pluginMux := http.NewServeMux()
	pluginMux.Handle("/", pluginHandler)
	pluginServer := &http.Server{Handler: pluginMux, ReadHeaderTimeout: 5 * time.Second}
	internalMux := http.NewServeMux()
	configured := config.reportWorker && components.worker != nil
	internalMux.HandleFunc("/internal/ready", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": true, "report_worker_configured": configured})
	})
	internalServer := &http.Server{Handler: internalMux, ReadHeaderTimeout: 3 * time.Second}

	workerCtx, cancelWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	if configured {
		go func() {
			defer close(workerDone)
			reportWorkerLoop(workerCtx, components.worker, config.pollInterval)
		}()
	} else {
		close(workerDone)
	}

	serverErrors := make(chan error, 2)
	go serveHTTP(pluginServer, pluginListener, serverErrors)
	go serveHTTP(internalServer, internalListener, serverErrors)

	var servingErr error
	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		servingErr = err
	}
	cancelWorker()

	var shutdownErr error
	select {
	case <-workerDone:
	case <-time.After(workerTimeout):
		shutdownErr = errors.New("report worker did not stop within its deadline")
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), serverTimeout)
	defer cancelShutdown()
	if err := pluginServer.Shutdown(shutdownCtx); err != nil && shutdownErr == nil {
		shutdownErr = errors.New("notification plugin HTTP requests did not drain")
	}
	if err := internalServer.Shutdown(shutdownCtx); err != nil && shutdownErr == nil {
		shutdownErr = errors.New("private readiness HTTP requests did not drain")
	}
	if shutdownErr != nil {
		closeListeners(pluginListener, internalListener)
		return shutdownErr
	}
	if components.ledger != nil {
		if err := components.ledger.Close(); err != nil {
			return errors.New("encrypted send ledger did not close cleanly")
		}
	}
	if servingErr != nil && !errors.Is(servingErr, http.ErrServerClosed) {
		return errors.New("notification HTTP server stopped unexpectedly")
	}
	return nil
}

func reportWorkerLoop(ctx context.Context, worker reportWorker, interval time.Duration) {
	if worker == nil {
		return
	}
	for {
		err := worker.RunOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		wait := interval
		if err != nil {
			log.Printf("Alibaba SMS report worker pass failed; message retained")
			wait = maxDuration(interval, 5*time.Second)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func serveHTTP(server *http.Server, listener net.Listener, serverErrors chan<- error) {
	err := server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		serverErrors <- err
		return
	}
	serverErrors <- http.ErrServerClosed
}

func readOperatorRoutes(path string, allowedOrigins map[string]bool) ([]adapter.ReceiptRoute, error) {
	data, err := readPrivateFile(path, maxRoutesConfigBytes)
	if err != nil || len(data) == 0 {
		return nil, errors.New("receipt route file must be a private regular file")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return nil, errors.New("receipt route file contains duplicate JSON keys")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var values []map[string]json.RawMessage
	if err := decoder.Decode(&values); err != nil || values == nil || len(values) == 0 || len(values) > 1000 {
		return nil, errors.New("receipt route file is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("receipt route file has trailing data")
	}
	allowedFields := map[string]bool{"tenant_id": true, "instance_id": true, "core_origin": true, "receipt_token_file": true}
	routes := make([]adapter.ReceiptRoute, 0, len(values))
	for _, value := range values {
		if len(value) != len(allowedFields) {
			return nil, errors.New("receipt route file has an incomplete entry")
		}
		for field := range value {
			if !allowedFields[field] {
				return nil, errors.New("receipt route file contains an unsupported field")
			}
		}
		var entry struct {
			TenantID         string
			InstanceID       string
			CoreOrigin       string
			ReceiptTokenFile string
		}
		for field, target := range map[string]*string{"tenant_id": &entry.TenantID, "instance_id": &entry.InstanceID, "core_origin": &entry.CoreOrigin, "receipt_token_file": &entry.ReceiptTokenFile} {
			if err := json.Unmarshal(value[field], target); err != nil || strings.TrimSpace(*target) == "" {
				return nil, errors.New("receipt route file contains an invalid value")
			}
		}
		origin := strings.TrimSpace(entry.CoreOrigin)
		if !validRouteID(entry.TenantID) || !validRouteID(entry.InstanceID) {
			return nil, errors.New("receipt route scope is invalid")
		}
		if !allowedOrigins[origin] || !validHTTPSOrigin(origin) {
			return nil, errors.New("receipt route origin is not allowlisted")
		}
		token, err := readPrivateFile(strings.TrimSpace(entry.ReceiptTokenFile), maxReceiptTokenBytes)
		if err != nil {
			return nil, errors.New("receipt token file must be private and regular")
		}
		tokenValue := strings.TrimSpace(string(token))
		if tokenValue == "" || strings.ContainsAny(tokenValue, "\r\n\t ") {
			return nil, errors.New("receipt token file is invalid")
		}
		routes = append(routes, adapter.ReceiptRoute{TenantID: entry.TenantID, InstanceID: entry.InstanceID, CoreURL: origin, ReceiptToken: tokenValue})
	}
	return routes, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return errors.New("duplicate object key")
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("unterminated JSON object")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("unterminated JSON array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func validRouteID(value string) bool {
	return value != "" && len(value) <= 128 && strings.TrimSpace(value) == value
}

func readPrivateFile(path string, maxBytes int) ([]byte, error) {
	if path == "" || maxBytes < 1 {
		return nil, errors.New("private file path is required")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errors.New("private file must be regular and mode 0600")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("could not open private file")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("private file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes+1)))
	if err != nil || len(data) > maxBytes {
		return nil, errors.New("private file exceeded its size limit")
	}
	return data, nil
}

func fixedMNSEndpoint(value, accountID, region string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.Port() != "" {
		return false
	}
	return parsed.Host == accountID+".mns."+region+".aliyuncs.com"
}

func validHTTPSOrigin(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Opaque == "" && parsed.Port() != "80" && parsed.Port() != "443"
}

func privateLoopbackAddress(value string) bool {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number >= 0 && number <= 65535
}

func envOr(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func splitNonEmpty(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			values = append(values, item)
		}
	}
	return values
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func closeListeners(listeners ...net.Listener) {
	for _, listener := range listeners {
		if listener != nil {
			_ = listener.Close()
		}
	}
}

func closeLedger(ledger ledgerCloser) {
	if ledger != nil {
		_ = ledger.Close()
	}
}
