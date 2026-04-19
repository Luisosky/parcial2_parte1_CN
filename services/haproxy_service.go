package services

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"vm-platform/models"
)

// HAProxyService handles HAProxy installation and configuration on an LB VM
type HAProxyService struct {
	VBox   *VBoxService
	SSHSvc *SSHService
}

func NewHAProxyService(vbox *VBoxService, sshSvc *SSHService) *HAProxyService {
	return &HAProxyService{VBox: vbox, SSHSvc: sshSvc}
}

// Install installs HAProxy on the target VM via SSH (as root, with password fallback)
func (h *HAProxyService) Install(lb *models.LoadBalancer) error {
	if err := h.ensureVMRunning(lb.VMName); err != nil {
		return err
	}
	port, err := h.VBox.GetNATPort(lb.VMName)
	if err != nil || port == "" {
		return fmt.Errorf("no se pudo obtener puerto SSH de la VM '%s'", lb.VMName)
	}

	rootKey := h.SSHSvc.GetPrivateKeyPath(lb.VMName, "root")

	// Forward the frontend port on the LB VM so the host can reach HAProxy
	h.ensurePortForward(lb.VMName, fmt.Sprintf("haproxy,tcp,,%d,,%d", lb.FrontendPort, lb.FrontendPort))
	// Forward stats port (9000 inside -> 9000 outside) so admin can see HAProxy stats
	h.ensurePortForward(lb.VMName, "haproxystats,tcp,,9000,,9000")

	// Install haproxy (idempotent)
	installCmd := `export DEBIAN_FRONTEND=noninteractive; if ! command -v haproxy >/dev/null 2>&1; then apt-get update -y && apt-get install -y haproxy; else echo 'haproxy ya instalado'; fi`
	if err := runSSHCommandWithKeyOrPassword(port, "root", rootKey, lb.RootPassword, installCmd); err != nil {
		return fmt.Errorf("error al instalar HAProxy: %v", err)
	}

	// Install net-tools / psmisc for metrics (top, kill)
	_ = runSSHCommandWithKeyOrPassword(port, "root", rootKey, lb.RootPassword, "apt-get install -y procps >/dev/null 2>&1 || true")

	// Enable at boot
	_ = runSSHCommandWithKeyOrPassword(port, "root", rootKey, lb.RootPassword, "systemctl enable haproxy")

	lb.Installed = true
	return nil
}

// ApplyConfig writes /etc/haproxy/haproxy.cfg based on the LB state and reloads haproxy
func (h *HAProxyService) ApplyConfig(lb *models.LoadBalancer) error {
	if err := h.ensureVMRunning(lb.VMName); err != nil {
		return err
	}
	port, err := h.VBox.GetNATPort(lb.VMName)
	if err != nil || port == "" {
		return fmt.Errorf("no se pudo obtener puerto SSH de la VM '%s'", lb.VMName)
	}
	rootKey := h.SSHSvc.GetPrivateKeyPath(lb.VMName, "root")

	cfg := h.buildConfig(lb)

	// Upload the config via SSH pipe
	if err := h.uploadText(port, "root", rootKey, lb.RootPassword, cfg, "/etc/haproxy/haproxy.cfg"); err != nil {
		return fmt.Errorf("error al subir haproxy.cfg: %v", err)
	}

	// Validate config before reload
	if err := runSSHCommandWithKeyOrPassword(port, "root", rootKey, lb.RootPassword,
		"haproxy -c -f /etc/haproxy/haproxy.cfg"); err != nil {
		return fmt.Errorf("configuración HAProxy inválida: %v", err)
	}

	// Reload (starts if stopped, reloads if running)
	reloadCmd := "systemctl is-active --quiet haproxy && systemctl reload haproxy || systemctl start haproxy"
	if err := runSSHCommandWithKeyOrPassword(port, "root", rootKey, lb.RootPassword, reloadCmd); err != nil {
		return fmt.Errorf("error al recargar HAProxy: %v", err)
	}
	lb.Running = true
	return nil
}

// Action: start/stop/restart haproxy
func (h *HAProxyService) Action(lb *models.LoadBalancer, action string) error {
	port, err := h.VBox.GetNATPort(lb.VMName)
	if err != nil || port == "" {
		return fmt.Errorf("no se pudo obtener puerto SSH")
	}
	rootKey := h.SSHSvc.GetPrivateKeyPath(lb.VMName, "root")

	var cmd string
	switch action {
	case "start":
		cmd = "systemctl start haproxy"
	case "stop":
		cmd = "systemctl stop haproxy"
	case "restart":
		cmd = "systemctl restart haproxy"
	case "status":
		cmd = "systemctl status haproxy --no-pager"
	default:
		return fmt.Errorf("acción inválida: %s", action)
	}
	if err := runSSHCommandWithKeyOrPassword(port, "root", rootKey, lb.RootPassword, cmd); err != nil {
		return err
	}
	if action == "start" || action == "restart" {
		lb.Running = true
	}
	if action == "stop" {
		lb.Running = false
	}
	return nil
}

// Status returns systemctl status text for haproxy
func (h *HAProxyService) Status(lb *models.LoadBalancer) string {
	port, err := h.VBox.GetNATPort(lb.VMName)
	if err != nil || port == "" {
		return ""
	}
	rootKey := h.SSHSvc.GetPrivateKeyPath(lb.VMName, "root")
	return getSSHOutputWithPassword(port, "root", rootKey, lb.RootPassword,
		"systemctl status haproxy --no-pager 2>/dev/null || true")
}

// buildConfig assembles the HAProxy config text
func (h *HAProxyService) buildConfig(lb *models.LoadBalancer) string {
	algo := lb.Algorithm
	if algo == "" {
		algo = "roundrobin"
	}
	var sb strings.Builder
	sb.WriteString("# Generado por VM Platform - Parte 3 Elasticidad\n")
	sb.WriteString("global\n")
	sb.WriteString("    log /dev/log local0\n")
	sb.WriteString("    log /dev/log local1 notice\n")
	sb.WriteString("    daemon\n")
	sb.WriteString("    maxconn 2048\n\n")
	sb.WriteString("defaults\n")
	sb.WriteString("    log global\n")
	sb.WriteString("    mode http\n")
	sb.WriteString("    option httplog\n")
	sb.WriteString("    option dontlognull\n")
	sb.WriteString("    timeout connect 5s\n")
	sb.WriteString("    timeout client  30s\n")
	sb.WriteString("    timeout server  30s\n")
	sb.WriteString("    retries 3\n\n")

	// Stats page
	sb.WriteString("listen stats\n")
	sb.WriteString("    bind *:9000\n")
	sb.WriteString("    mode http\n")
	sb.WriteString("    stats enable\n")
	sb.WriteString("    stats uri /\n")
	sb.WriteString("    stats refresh 5s\n")
	sb.WriteString("    stats realm HAProxy\\ Statistics\n")
	sb.WriteString("    stats auth admin:admin\n\n")

	// Frontend
	sb.WriteString(fmt.Sprintf("frontend ft_%s\n", sanitize(lb.Name)))
	sb.WriteString(fmt.Sprintf("    bind *:%d\n", lb.FrontendPort))
	sb.WriteString(fmt.Sprintf("    default_backend bk_%s\n\n", sanitize(lb.Name)))

	// Backend
	sb.WriteString(fmt.Sprintf("backend bk_%s\n", sanitize(lb.Name)))
	sb.WriteString(fmt.Sprintf("    balance %s\n", algo))
	sb.WriteString("    option httpchk GET /\n")
	for _, b := range lb.Backends {
		if b == nil || !b.Enabled {
			continue
		}
		weight := b.Weight
		if weight <= 0 {
			weight = 1
		}
		sb.WriteString(fmt.Sprintf("    server %s %s:%d check weight %d\n",
			sanitize(b.Name), b.Address, b.Port, weight))
	}
	sb.WriteString("\n")
	return sb.String()
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			out = append(out, byte(r))
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

func (h *HAProxyService) ensureVMRunning(vmName string) error {
	state, _ := h.VBox.GetVMState(vmName)
	if state != "running" {
		if err := h.VBox.StartVM(vmName); err != nil {
			return fmt.Errorf("error al iniciar la VM '%s': %v", vmName, err)
		}
		time.Sleep(30 * time.Second)
	}
	return nil
}

// ensurePortForward adds a NAT port-forward rule if not present
func (h *HAProxyService) ensurePortForward(vmName, rule string) {
	// Rule format: name,proto,hostIp,hostPort,guestIp,guestPort
	parts := strings.Split(rule, ",")
	if len(parts) < 1 {
		return
	}
	name := parts[0]
	// Remove existing rule silently (ignore failure)
	_, _ = h.VBox.runCommand("modifyvm", vmName, "--natpf1", "delete", name)
	// Add it
	_, _ = h.VBox.runCommand("modifyvm", vmName, "--natpf1", rule)
}

// uploadText uploads a text file to the remote host over SSH
func (h *HAProxyService) uploadText(port, username, keyPath, password, content, remotePath string) error {
	var authMethods []ssh.AuthMethod
	if keyBytes, err := os.ReadFile(keyPath); err == nil {
		if signer, err := ssh.ParsePrivateKey(keyBytes); err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}
	if len(authMethods) == 0 {
		return fmt.Errorf("sin credenciales SSH")
	}
	cfg := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	}
	client, err := ssh.Dial("tcp", "127.0.0.1:"+port, cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = bytes.NewReader([]byte(content))
	return session.Run(fmt.Sprintf("cat > '%s'", remotePath))
}
