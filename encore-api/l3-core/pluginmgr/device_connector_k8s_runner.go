package pluginmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"
)

// K8SRunner implements Runner using kubectl to manage K8S resources.
// It does NOT use k8s.io/client-go to keep the dependency surface minimal.
// All resource creation/deletion goes through kubectl apply/delete.
//
// Resource naming rules:
//   - All resources use label: app.kubernetes.io/managed-by=device-connector-runner
//   - Names are deterministic: connector-<instanceID[:8]>
//   - Secrets are never logged or stored in connector_events.
//
// Constraints (from T007 spec):
//   - Must NOT know about protocol_type.
//   - Must NOT call ThingsPanel backend directly.
//   - Must NOT encode adapter-specific behavior.
//   - Must NOT hide failed pods behind a generic error.
type K8SRunner struct {
	// KubeconfigPath is the path to the kubeconfig file.
	// Empty = use in-cluster config or default kubeconfig.
	KubeconfigPath string

	// KubectlPath is the path to kubectl binary. Default: "kubectl".
	KubectlPath string

	// DefaultNamespace is used when ConnectorInstance.Namespace is empty.
	DefaultNamespace string

	// StatusPollInterval is how often to poll for Deployment readiness.
	// Default: 2 seconds.
	StatusPollInterval time.Duration

	// StatusPollTimeout is the maximum time to wait for readiness.
	// Default: 120 seconds.
	StatusPollTimeout time.Duration

	// cmdRunner allows injecting a fake in tests.
	cmdRunner CommandRunner
}

// NewK8SRunner creates a K8SRunner with sensible defaults.
func NewK8SRunner(kubeconfigPath string) *K8SRunner {
	return &K8SRunner{
		KubeconfigPath:     kubeconfigPath,
		KubectlPath:        "kubectl",
		DefaultNamespace:   "connector-instances",
		StatusPollInterval: 2 * time.Second,
		StatusPollTimeout:  120 * time.Second,
		cmdRunner:          osRunner{},
	}
}

// Reconcile creates or updates K8S resources for the given instance.
// Resources created:
//   - ConfigMap: non-secret env vars
//   - Secret: secret env vars (none in MVP; placeholder created empty)
//   - Deployment: connector container
//   - Service: HTTP port (if ConnectorRuntime.HTTPPort > 0)
//
// On success: returns Running status with httpAddress = ClusterIP:HTTPPort.
// On failure: returns Error status with pod failure reason.
func (r *K8SRunner) Reconcile(ctx context.Context, instance ConnectorInstance, runtime ConnectorRuntime) (ConnectorStatus, error) {
	ns := r.namespace(instance)
	name := r.resourceName(instance.ID)

	// 1. Ensure namespace exists.
	if err := r.ensureNamespace(ctx, ns); err != nil {
		return r.errorStatus(instance.ID, fmt.Sprintf("ensure namespace %q: %v", ns, err)), err
	}

	// 2. Apply ConfigMap (non-secret env vars).
	cmManifest, err := r.buildConfigMapManifest(name, ns, instance, runtime)
	if err != nil {
		return r.errorStatus(instance.ID, "build ConfigMap: "+err.Error()), err
	}
	if applyErr := r.kubectlApplyStdin(ctx, cmManifest); applyErr != nil {
		return r.errorStatus(instance.ID, "apply ConfigMap: "+applyErr.Error()), applyErr
	}

	// 3. Apply Secret (empty placeholder; secrets injected by operator in production).
	secretManifest := r.buildSecretManifest(name, ns, instance)
	if applyErr := r.kubectlApplyStdin(ctx, secretManifest); applyErr != nil {
		return r.errorStatus(instance.ID, "apply Secret: "+applyErr.Error()), applyErr
	}

	// 4. Apply Deployment.
	deployManifest, err := r.buildDeploymentManifest(name, ns, instance, runtime)
	if err != nil {
		return r.errorStatus(instance.ID, "build Deployment: "+err.Error()), err
	}
	if applyErr := r.kubectlApplyStdin(ctx, deployManifest); applyErr != nil {
		return r.errorStatus(instance.ID, "apply Deployment: "+applyErr.Error()), applyErr
	}

	// 5. Apply Service (only if HTTP port is configured).
	var httpAddress string
	if runtime.HTTPPort > 0 {
		svcManifest := r.buildServiceManifest(name, ns, instance, runtime)
		if applyErr := r.kubectlApplyStdin(ctx, svcManifest); applyErr != nil {
			return r.errorStatus(instance.ID, "apply Service: "+applyErr.Error()), applyErr
		}

		// 6. Resolve ClusterIP.
		clusterIP, ipErr := r.waitForServiceClusterIP(ctx, name, ns)
		if ipErr != nil {
			return r.errorStatus(instance.ID, "wait for ClusterIP: "+ipErr.Error()), ipErr
		}
		httpAddress = fmt.Sprintf("%s:%d", clusterIP, runtime.HTTPPort)
	}

	// 7. Wait for Deployment readiness.
	if readyErr := r.waitForDeploymentReady(ctx, name, ns); readyErr != nil {
		// Read pod failure reason for actionable error.
		reason := r.getPodFailureReason(ctx, instance.ID, ns)
		msg := fmt.Sprintf("deployment not ready: %v; pod reason: %s", readyErr, reason)
		return r.errorStatus(instance.ID, msg), fmt.Errorf("%s", msg)
	}

	hpa := time.Now().UTC()
	return ConnectorStatus{
		ConnectorInstanceID: instance.ID,
		Phase:               PhaseRunning,
		ProcessHealthy:      true,
		LastHeartbeatAt:     &hpa,
		HTTPAddress:         httpAddress,
		Message:             fmt.Sprintf("K8S Deployment %s/%s ready; httpAddress=%s", ns, name, httpAddress),
		UpdatedAt:           time.Now().UTC(),
	}, nil
}

// Delete removes all K8S resources for the given instance using label selector.
func (r *K8SRunner) Delete(ctx context.Context, instanceID string) error {
	name := r.resourceName(instanceID)
	ns := r.DefaultNamespace // best-effort; the real namespace should be stored in ConnectorInstance

	label := fmt.Sprintf("device-connector-instance=%s", instanceID)

	// Delete all labeled resources in one call.
	_, err := r.kubectl(ctx, "delete",
		"deployment,service,configmap,secret",
		"-n", ns,
		"-l", label,
		"--ignore-not-found=true",
	)
	if err != nil {
		// Try by name as fallback.
		for _, kind := range []string{"deployment", "service", "configmap", "secret"} {
			_, _ = r.kubectl(ctx, "delete", kind, name, "-n", ns, "--ignore-not-found=true")
		}
	}
	return nil
}

// Logs returns recent log lines from the connector pod.
func (r *K8SRunner) Logs(ctx context.Context, instanceID string, opts LogOptions) ([]string, error) {
	name := r.resourceName(instanceID)
	ns := r.DefaultNamespace

	args := []string{"logs", "-n", ns, "-l", "device-connector-instance=" + instanceID, "--all-containers=true"}
	if opts.TailLines > 0 {
		args = append(args, fmt.Sprintf("--tail=%d", opts.TailLines))
	}

	out, err := r.kubectl(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("kubectl logs for %s/%s: %w", ns, name, err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines, nil
}

// ─── Manifest builders ────────────────────────────────────────────────────────

const configMapTemplate = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/managed-by: device-connector-runner
    device-connector-instance: {{.InstanceID}}
    device-connector-id: {{.ConnectorID}}
data:
{{- range $k, $v := .EnvVars}}
  {{$k}}: {{$v | printf "%q"}}
{{- end}}
`

const secretTemplate = `apiVersion: v1
kind: Secret
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/managed-by: device-connector-runner
    device-connector-instance: {{.InstanceID}}
type: Opaque
data: {}
`

const deploymentTemplate = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/managed-by: device-connector-runner
    device-connector-instance: {{.InstanceID}}
    device-connector-id: {{.ConnectorID}}
spec:
  replicas: 1
  selector:
    matchLabels:
      device-connector-instance: {{.InstanceID}}
  template:
    metadata:
      labels:
        device-connector-instance: {{.InstanceID}}
        device-connector-id: {{.ConnectorID}}
    spec:
      containers:
      - name: connector
        image: {{.Image}}
        envFrom:
        - configMapRef:
            name: {{.Name}}
        - secretRef:
            name: {{.Name}}
            optional: true
{{- if .HTTPPort}}
        ports:
        - containerPort: {{.HTTPPort}}
          name: http
        readinessProbe:
          httpGet:
            path: {{.HealthCheckPath}}
            port: {{.HTTPPort}}
          initialDelaySeconds: 5
          periodSeconds: 5
          failureThreshold: 6
        livenessProbe:
          httpGet:
            path: {{.HealthCheckPath}}
            port: {{.HTTPPort}}
          initialDelaySeconds: 15
          periodSeconds: 10
{{- end}}
{{- if .ResourceLimits}}
        resources:
          requests:
            cpu: {{.ResourceLimits.CPURequest}}
            memory: {{.ResourceLimits.MemoryRequest}}
          limits:
            cpu: {{.ResourceLimits.CPULimit}}
            memory: {{.ResourceLimits.MemoryLimit}}
{{- end}}
`

const serviceTemplate = `apiVersion: v1
kind: Service
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/managed-by: device-connector-runner
    device-connector-instance: {{.InstanceID}}
spec:
  selector:
    device-connector-instance: {{.InstanceID}}
  ports:
  - name: http
    port: {{.HTTPPort}}
    targetPort: {{.HTTPPort}}
  type: ClusterIP
`

func (r *K8SRunner) buildConfigMapManifest(name, ns string, instance ConnectorInstance, runtime ConnectorRuntime) ([]byte, error) {
	t := template.Must(template.New("cm").Parse(configMapTemplate))
	envVars := make(map[string]string, len(runtime.EnvVars)+2)
	for k, v := range runtime.EnvVars {
		envVars[k] = v
	}
	if _, ok := envVars["CONNECTOR_INSTANCE_ID"]; !ok {
		envVars["CONNECTOR_INSTANCE_ID"] = instance.ID
	}
	if runtime.HTTPPort > 0 {
		if _, ok := envVars["CONNECTOR_LISTEN_ADDR"]; !ok {
			envVars["CONNECTOR_LISTEN_ADDR"] = fmt.Sprintf(":%d", runtime.HTTPPort)
		}
	}
	data := struct {
		Name        string
		Namespace   string
		InstanceID  string
		ConnectorID string
		EnvVars     map[string]string
	}{
		Name:        name,
		Namespace:   ns,
		InstanceID:  instance.ID,
		ConnectorID: runtime.DeviceConnectorID,
		EnvVars:     envVars,
	}
	if data.EnvVars == nil {
		data.EnvVars = map[string]string{}
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *K8SRunner) buildSecretManifest(name, ns string, instance ConnectorInstance) []byte {
	t := template.Must(template.New("secret").Parse(secretTemplate))
	data := struct {
		Name       string
		Namespace  string
		InstanceID string
	}{Name: name, Namespace: ns, InstanceID: instance.ID}
	var buf bytes.Buffer
	_ = t.Execute(&buf, data)
	return buf.Bytes()
}

func (r *K8SRunner) buildDeploymentManifest(name, ns string, instance ConnectorInstance, runtime ConnectorRuntime) ([]byte, error) {
	t := template.Must(template.New("deploy").Parse(deploymentTemplate))

	hcp := runtime.HealthCheckPath
	if hcp == "" {
		hcp = "/health"
	}

	data := struct {
		Name            string
		Namespace       string
		InstanceID      string
		ConnectorID     string
		Image           string
		HTTPPort        int
		HealthCheckPath string
		ResourceLimits  *ResourceLimits
	}{
		Name:            name,
		Namespace:       ns,
		InstanceID:      instance.ID,
		ConnectorID:     instance.DeviceConnectorID,
		Image:           runtime.Image,
		HTTPPort:        runtime.HTTPPort,
		HealthCheckPath: hcp,
		ResourceLimits:  runtime.ResourceLimits,
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *K8SRunner) buildServiceManifest(name, ns string, instance ConnectorInstance, runtime ConnectorRuntime) []byte {
	t := template.Must(template.New("svc").Parse(serviceTemplate))
	data := struct {
		Name       string
		Namespace  string
		InstanceID string
		HTTPPort   int
	}{Name: name, Namespace: ns, InstanceID: instance.ID, HTTPPort: runtime.HTTPPort}
	var buf bytes.Buffer
	_ = t.Execute(&buf, data)
	return buf.Bytes()
}

// ─── kubectl helpers ──────────────────────────────────────────────────────────

func (r *K8SRunner) kubectl(ctx context.Context, args ...string) ([]byte, error) {
	allArgs := r.kubectlArgs(args...)
	runner := r.cmdRunner
	if runner == nil {
		runner = osRunner{}
	}
	return runner.Run(ctx, r.kubectlBin(), allArgs...)
}

// kubectlApplyStdin writes the manifest bytes to a temp file and runs
// "kubectl apply -f <file>". Using a temp file instead of piping stdin keeps
// the CommandRunner interface simple (no Stdin field needed) while producing
// identical behaviour — kubectl reads the file the same way.
func (r *K8SRunner) kubectlApplyStdin(ctx context.Context, manifest []byte) error {
	// Create temp file in the OS default temp dir.
	f, err := os.CreateTemp("", "dc-manifest-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp manifest file: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath) // always clean up

	if _, err := f.Write(manifest); err != nil {
		f.Close()
		return fmt.Errorf("write manifest to temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp manifest file: %w", err)
	}

	_, applyErr := r.kubectl(ctx, "apply", "-f", tmpPath)
	return applyErr
}

func (r *K8SRunner) ensureNamespace(ctx context.Context, ns string) error {
	// "kubectl create namespace <ns> --dry-run=client" just validates; use
	// apply so the namespace is actually created if it doesn't exist yet.
	nsManifest := fmt.Appendf(nil,
		"apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n  labels:\n    app.kubernetes.io/managed-by: device-connector-runner\n",
		ns,
	)
	return r.kubectlApplyStdin(ctx, nsManifest)
}

func (r *K8SRunner) waitForDeploymentReady(ctx context.Context, name, ns string) error {
	timeout := r.StatusPollTimeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	interval := r.StatusPollInterval
	if interval == 0 {
		interval = 2 * time.Second
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		out, err := r.kubectl(ctx,
			"get", "deployment", name,
			"-n", ns,
			"-o", `jsonpath={.status.readyReplicas}`,
		)
		if err == nil {
			ready := strings.TrimSpace(string(out))
			if ready == "1" {
				return nil
			}
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("timeout waiting for deployment %s/%s to become ready", ns, name)
}

func (r *K8SRunner) waitForServiceClusterIP(ctx context.Context, name, ns string) (string, error) {
	timeout := 30 * time.Second
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		out, err := r.kubectl(ctx,
			"get", "service", name,
			"-n", ns,
			"-o", `jsonpath={.spec.clusterIP}`,
		)
		if err == nil {
			ip := strings.TrimSpace(string(out))
			if ip != "" && ip != "None" {
				return ip, nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return "", fmt.Errorf("timeout waiting for Service %s/%s ClusterIP", ns, name)
}

func (r *K8SRunner) getPodFailureReason(ctx context.Context, instanceID, ns string) string {
	// Query pod status for the deployment's pods to get a human-readable failure reason.
	// Do NOT log any secret values.
	out, err := r.kubectl(ctx,
		"get", "pods",
		"-n", ns,
		"-l", fmt.Sprintf("device-connector-instance=%s", instanceID),
		"-o", `jsonpath={range .items[*]}{.status.containerStatuses[*].state.waiting.reason}{"\t"}{.status.containerStatuses[*].state.waiting.message}{"\n"}{end}`,
	)
	if err != nil || len(out) == 0 {
		return "unknown (kubectl error or no pods)"
	}
	// Strip secrets from message — only return reason code, not full message.
	fields := strings.Fields(string(out))
	if len(fields) > 0 {
		return fields[0] // reason code only, e.g. "ImagePullBackOff", "CrashLoopBackOff"
	}
	return strings.TrimSpace(string(out))
}

func (r *K8SRunner) kubectlBin() string {
	if r.KubectlPath != "" {
		return r.KubectlPath
	}
	return "kubectl"
}

func (r *K8SRunner) kubectlArgs(args ...string) []string {
	var prefix []string
	if r.KubeconfigPath != "" {
		prefix = append(prefix, "--kubeconfig="+r.KubeconfigPath)
	}
	return append(prefix, args...)
}

func (r *K8SRunner) namespace(instance ConnectorInstance) string {
	if instance.Namespace != "" {
		return instance.Namespace
	}
	if r.DefaultNamespace != "" {
		return r.DefaultNamespace
	}
	return "connector-instances"
}

func (r *K8SRunner) resourceName(instanceID string) string {
	// Safe K8S name: "connector-" + first 8 chars of UUID.
	id := strings.ReplaceAll(instanceID, "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	return "connector-" + strings.ToLower(id)
}

func (r *K8SRunner) errorStatus(instanceID, msg string) ConnectorStatus {
	return ConnectorStatus{
		ConnectorInstanceID: instanceID,
		Phase:               PhaseError,
		ProcessHealthy:      false,
		Message:             msg,
		UpdatedAt:           time.Now().UTC(),
	}
}

// ─── K8S runner status summary (for connector_events) ────────────────────────

// K8SResourceRefs holds references to K8S resources created for an instance.
// Stored in connector_events.message as JSON to keep status/events traceable.
// Secrets are NEVER included here.
type K8SResourceRefs struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	Service    string `json:"service,omitempty"`
	ConfigMap  string `json:"configMap"`
}

// MarshalK8SResourceRefs serialises resource refs to JSON for event messages.
// Guaranteed not to contain secret values.
func MarshalK8SResourceRefs(refs K8SResourceRefs) string {
	b, _ := json.Marshal(refs)
	return string(b)
}
