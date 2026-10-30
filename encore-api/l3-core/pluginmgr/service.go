package pluginmgr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"encore.app/internal/authz"
	"encore.app/internal/legacy"
	"encore.app/l1-access/authn"
)

const (
	defaultRepoURL   = "https://github.com/ThingsPanel/thingspanel-adapter-tcp.git"
	defaultBranch    = "main"
	defaultPluginDir = "/opt/thingspanel/plugins"
)

//encore:service
type Service struct {
	runner CommandRunner
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type osRunner struct{}

func (osRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

type InstallRequest struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	User            string `json:"user"`
	PrivateKeyPath  string `json:"private_key_path"`
	CertificatePath string `json:"certificate_path"`
	KnownHostsPath  string `json:"known_hosts_path"`

	RepoURL        string `json:"repo_url"`
	Branch         string `json:"branch"`
	PluginDir      string `json:"plugin_dir"`
	PluginName     string `json:"plugin_name"`
	ConfigJSON     string `json:"config_json"`
	ConfigPath     string `json:"config_path"`
	RestartCommand string `json:"restart_command"`

	RegisterInTP              bool   `json:"register_in_thingspanel"`
	RegisterName              string `json:"register_name"`
	RegisterDeviceType        int16  `json:"register_device_type"`
	RegisterProtocolType      string `json:"register_protocol_type"`
	RegisterServiceAccess     string `json:"register_service_access_address"`
	RegisterDescription       string `json:"register_description"`
	RegisterAdditionalInfo    string `json:"register_additional_info"`
	RegisterLanguageCode      string `json:"register_language_code"`
	RegisterServiceIdentifier string `json:"register_service_identifier"`
	RegisterServiceVersion    string `json:"register_service_version"`
	RegisterServiceHeartbeat  int    `json:"register_service_heartbeat"`
	RegisterServiceOnly       bool   `json:"register_service_only"`
	SkipInstall               bool   `json:"skip_install"`

	DryRun bool `json:"dry_run"`
}

type InstallResponse struct {
	Code           int      `json:"code"`
	Message        string   `json:"message"`
	RequestID      string   `json:"requestId"`
	PluginName     string   `json:"plugin_name"`
	Plan           []string `json:"plan"`
	Outputs        []string `json:"outputs"`
	RegisterResult string   `json:"register_result,omitempty"`
	ServiceResult  string   `json:"service_result,omitempty"`
}

// InstallProtocolPlugin installs/configures protocol plugin through SSH.
//
//encore:api auth method=POST path=/v1/plugins/protocol/install
func (s *Service) InstallProtocolPlugin(ctx context.Context, req *InstallRequest) (*InstallResponse, error) {
	ad := authn.CurrentData()
	if ad == nil {
		return nil, fmt.Errorf("auth required")
	}
	if !authz.Can(ad, "plugin", "manage") {
		return nil, fmt.Errorf("no plugin manage permission")
	}
	if req == nil {
		return nil, fmt.Errorf("request required")
	}
	if strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.User) == "" || strings.TrimSpace(req.PrivateKeyPath) == "" {
		return nil, fmt.Errorf("host/user/private_key_path are required")
	}

	repoURL := strings.TrimSpace(req.RepoURL)
	if repoURL == "" {
		repoURL = defaultRepoURL
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		branch = defaultBranch
	}
	pluginDir := strings.TrimSpace(req.PluginDir)
	if pluginDir == "" {
		pluginDir = defaultPluginDir
	}
	pluginName := strings.TrimSpace(req.PluginName)
	if pluginName == "" {
		pluginName = inferPluginName(repoURL)
	}

	installScript := buildInstallScript(repoURL, branch, pluginDir, pluginName)
	steps := []string{"install-plugin"}
	scripts := []string{installScript}
	if req.SkipInstall {
		steps = []string{}
		scripts = []string{}
	}

	if strings.TrimSpace(req.ConfigJSON) != "" && !req.SkipInstall {
		cfgPath := strings.TrimSpace(req.ConfigPath)
		if cfgPath == "" {
			return nil, fmt.Errorf("config_path required when config_json is provided")
		}
		steps = append(steps, "write-config")
		scripts = append(scripts, buildConfigScript(cfgPath, req.ConfigJSON))
	}
	if strings.TrimSpace(req.RestartCommand) != "" && !req.SkipInstall {
		steps = append(steps, "restart-service")
		scripts = append(scripts, strings.TrimSpace(req.RestartCommand))
	}

	plan := make([]string, 0, len(steps))
	outputs := make([]string, 0, len(steps))
	runner := s.runner
	if runner == nil {
		runner = osRunner{}
	}

	for i, step := range steps {
		sshArgs := buildSSHArgs(req, scripts[i])
		plan = append(plan, step+": ssh "+strings.Join(sshArgs, " "))
		if req.DryRun {
			continue
		}
		out, err := runner.Run(ctx, "ssh", sshArgs...)
		if err != nil {
			return nil, fmt.Errorf("%s failed: %w", step, err)
		}
		outputs = append(outputs, strings.TrimSpace(string(out)))
	}

	registerResult := ""
	serviceResult := ""
	if req.RegisterInTP {
		registerName := strings.TrimSpace(req.RegisterName)
		if registerName == "" {
			registerName = pluginName
		}
		registerDeviceType := req.RegisterDeviceType
		if registerDeviceType == 0 {
			registerDeviceType = 2
		}
		registerProtocolType := strings.TrimSpace(req.RegisterProtocolType)
		if registerProtocolType == "" {
			registerProtocolType = "tcp"
		}
		registerServiceAccess := strings.TrimSpace(req.RegisterServiceAccess)
		if registerServiceAccess == "" {
			return nil, fmt.Errorf("register_service_access_address required when register_in_thingspanel=true")
		}
		registerDescription := strings.TrimSpace(req.RegisterDescription)
		if registerDescription == "" {
			registerDescription = "installed by encore plugin installer"
		}
		registerAdditionalInfo := strings.TrimSpace(req.RegisterAdditionalInfo)
		if registerAdditionalInfo == "" {
			registerAdditionalInfo = "{}"
		}
		registerLanguage := strings.TrimSpace(req.RegisterLanguageCode)
		if registerLanguage == "" {
			registerLanguage = "zh-CN"
		}
		registerServiceIdentifier := strings.TrimSpace(req.RegisterServiceIdentifier)
		if registerServiceIdentifier == "" {
			registerServiceIdentifier = inferServiceIdentifier(req.ConfigJSON)
		}
		if registerServiceIdentifier == "" {
			registerServiceIdentifier = registerProtocolType
		}
		registerServiceVersion := strings.TrimSpace(req.RegisterServiceVersion)
		if registerServiceVersion == "" {
			registerServiceVersion = "v1.0.0"
		}
		registerServiceHeartbeat := req.RegisterServiceHeartbeat
		if registerServiceHeartbeat <= 0 {
			registerServiceHeartbeat = 1
		}
		serviceConfig, err := buildServiceConfigJSON(registerServiceAccess, registerServiceIdentifier, registerDeviceType)
		if err != nil {
			return nil, fmt.Errorf("build service_config failed: %w", err)
		}

		if req.RegisterServiceOnly {
			plan = append(plan, "skip-protocol-register: register_service_only=true")
		} else {
			plan = append(plan, "register-plugin: POST /api/v1/protocol_plugin")
		}
		plan = append(plan, "upsert-service: POST/PUT /api/v1/service")
		if !req.DryRun {
			cli := legacy.NewFromEnv()
			if !req.RegisterServiceOnly {
				raw, err := cli.CreateProtocolPlugin(ctx, legacy.ProtocolPluginCreateReq{
					Name:                 registerName,
					DeviceType:           registerDeviceType,
					ProtocolType:         registerProtocolType,
					ServiceAccessAddress: registerServiceAccess,
					Description:          registerDescription,
					AdditionalInfo:       registerAdditionalInfo,
					LanguageCode:         registerLanguage,
				})
				if err != nil {
					return nil, fmt.Errorf("register-plugin failed: %w", err)
				}
				registerResult = raw
			}
			serviceRaw, err := cli.UpsertServicePluginByIdentifier(ctx, legacy.ServicePluginUpsertReq{
				Name:              registerName,
				ServiceIdentifier: registerServiceIdentifier,
				ServiceType:       1,
				Description:       registerDescription,
				Version:           registerServiceVersion,
				ServiceHeartbeat:  registerServiceHeartbeat,
				ServiceConfig:     serviceConfig,
			})
			if err != nil {
				return nil, fmt.Errorf("upsert-service failed: %w", err)
			}
			serviceResult = serviceRaw
		}
	}

	return &InstallResponse{
		Code:           200,
		Message:        "ok",
		RequestID:      "",
		PluginName:     pluginName,
		Plan:           plan,
		Outputs:        outputs,
		RegisterResult: registerResult,
		ServiceResult:  serviceResult,
	}, nil
}

func buildSSHArgs(req *InstallRequest, remoteScript string) []string {
	port := req.Port
	if port <= 0 {
		port = 22
	}
	args := []string{"-p", strconv.Itoa(port), "-i", strings.TrimSpace(req.PrivateKeyPath), "-o", "IdentitiesOnly=yes"}
	if v := strings.TrimSpace(req.CertificatePath); v != "" {
		args = append(args, "-o", "CertificateFile="+v)
	}
	if v := strings.TrimSpace(req.KnownHostsPath); v != "" {
		args = append(args, "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile="+v)
	} else {
		args = append(args, "-o", "StrictHostKeyChecking=accept-new")
	}
	args = append(args, strings.TrimSpace(req.User)+"@"+strings.TrimSpace(req.Host), "bash -lc "+shQuote(remoteScript))
	return args
}

func buildInstallScript(repoURL, branch, pluginDir, pluginName string) string {
	target := strings.TrimRight(pluginDir, "/") + "/" + pluginName
	return strings.Join([]string{
		"set -e",
		"mkdir -p " + shQuote(pluginDir),
		"if [ -d " + shQuote(target+"/.git") + " ]; then",
		"  cd " + shQuote(target),
		"  git fetch --all",
		"  git checkout " + shQuote(branch),
		"  git pull --ff-only",
		"else",
		"  git clone --depth 1 --branch " + shQuote(branch) + " " + shQuote(repoURL) + " " + shQuote(target),
		"fi",
	}, "\n")
}

func buildConfigScript(configPath, configJSON string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(configJSON))
	return strings.Join([]string{
		"set -e",
		"mkdir -p " + shQuote(filepath.Dir(configPath)),
		"echo " + shQuote(encoded) + " | base64 -d > " + shQuote(configPath),
	}, "\n")
}

func inferServiceIdentifier(configJSON string) string {
	if strings.TrimSpace(configJSON) == "" {
		return ""
	}
	var cfg struct {
		Platform struct {
			ServiceIdentifier string `json:"service_identifier"`
		} `json:"platform"`
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Platform.ServiceIdentifier)
}

func normalizeHTTPAddress(addr string) string {
	a := strings.TrimSpace(addr)
	a = strings.TrimPrefix(a, "http://")
	a = strings.TrimPrefix(a, "https://")
	a = strings.TrimRight(a, "/")
	return a
}

func buildServiceConfigJSON(serviceAccessAddress, serviceIdentifier string, deviceType int16) (string, error) {
	cfg := map[string]any{
		"http_address":     normalizeHTTPAddress(serviceAccessAddress),
		"device_type":      deviceType,
		"sub_topic_prefix": "plugin/" + strings.TrimSpace(serviceIdentifier) + "/",
		"access_address":   "",
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func inferPluginName(repoURL string) string {
	r := strings.TrimSpace(repoURL)
	r = strings.TrimSuffix(r, "/")
	r = strings.TrimSuffix(r, ".git")
	idx := strings.LastIndex(r, "/")
	if idx >= 0 && idx+1 < len(r) {
		return r[idx+1:]
	}
	return "thingspanel-adapter-tcp"
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
