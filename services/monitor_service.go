package services

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"vm-platform/models"
)

// MonitorService samples CPU usage on backend VMs via SSH and caches recent samples
type MonitorService struct {
	VBox   *VBoxService
	SSHSvc *SSHService
	mu     sync.RWMutex

	// history: key = VM name, value = circular list of measurements (newest last)
	history map[string][]models.CPUMetric
	// eventLog: last N scaling/monitoring events, newest first
	eventLog []string

	// per-LB high/low timers
	highSince map[string]time.Time // lb name -> time since upper threshold first exceeded sustainedly
	lowSince  map[string]time.Time // lb name -> time since lower threshold first crossed sustainedly
}

func NewMonitorService(vbox *VBoxService, sshSvc *SSHService) *MonitorService {
	return &MonitorService{
		VBox:      vbox,
		SSHSvc:    sshSvc,
		history:   make(map[string][]models.CPUMetric),
		eventLog:  make([]string, 0, 100),
		highSince: make(map[string]time.Time),
		lowSince:  make(map[string]time.Time),
	}
}

// SampleCPU reads CPU% from a VM via SSH and returns the used CPU percentage.
// rootPassword is used as auth fallback if SSH key auth is not available.
func (m *MonitorService) SampleCPU(vmName, username, rootPassword string) models.CPUMetric {
	metric := models.CPUMetric{
		VMName:    vmName,
		Timestamp: time.Now(),
	}

	if vmName == "" {
		metric.Error = "sin nombre de VM"
		return metric
	}

	state, _ := m.VBox.GetVMState(vmName)
	if state != "running" {
		metric.Error = "VM apagada"
		return metric
	}

	port, err := m.VBox.GetNATPort(vmName)
	if err != nil || port == "" {
		metric.Error = "sin puerto SSH"
		return metric
	}

	rootKey := m.SSHSvc.GetPrivateKeyPath(vmName, "root")
	userKey := m.SSHSvc.GetPrivateKeyPath(vmName, username)

	// Pure-awk CPU sampling: two /proc/stat reads 1 second apart.
	// LC_ALL=C forces dot as decimal separator regardless of VM locale.
	cmd := `A=$(awk '/^cpu /{print $2+$3+$4+$6+$7+$8, $5; exit}' /proc/stat); sleep 1; B=$(awk '/^cpu /{print $2+$3+$4+$6+$7+$8, $5; exit}' /proc/stat); LC_ALL=C awk -v a="$A" -v b="$B" 'BEGIN{split(a,x);split(b,y);du=y[1]-x[1];di=y[2]-x[2];tot=du+di;if(tot>0)printf "%.2f\n",(du*100)/tot;else print 0}'`

	// Try root key, then user key, then root password as last resort.
	out := getSSHOutputWithPassword(port, "root", rootKey, rootPassword, cmd)
	if strings.TrimSpace(out) == "" {
		out = getSSHOutputWithPassword(port, username, userKey, rootPassword, cmd)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		metric.Error = "SSH sin respuesta (verifica llaves/contraseña)"
		return metric
	}

	// Parse last numeric line (guards against warning lines in output)
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if v, err := strconv.ParseFloat(line, 64); err == nil {
			metric.CPUPercent = v
			return metric
		}
	}
	metric.Error = "output no numérico: " + out
	return metric
}

// RecordMetric saves a metric into history (capped at 60 entries per VM)
func (m *MonitorService) RecordMetric(metric models.CPUMetric) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.history[metric.VMName]
	list = append(list, metric)
	if len(list) > 60 {
		list = list[len(list)-60:]
	}
	m.history[metric.VMName] = list
}

// GetHistory returns the history for a VM
func (m *MonitorService) GetHistory(vmName string) []models.CPUMetric {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]models.CPUMetric, len(m.history[vmName]))
	copy(out, m.history[vmName])
	return out
}

// GetLatest returns the most recent metric for each VM in the list
func (m *MonitorService) GetLatest(vmNames []string) []models.CPUMetric {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]models.CPUMetric, 0, len(vmNames))
	for _, name := range vmNames {
		list := m.history[name]
		if len(list) > 0 {
			out = append(out, list[len(list)-1])
		} else {
			out = append(out, models.CPUMetric{VMName: name, Error: "sin muestras"})
		}
	}
	return out
}

// LogEvent prepends an event to the ring log
func (m *MonitorService) LogEvent(msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	m.eventLog = append([]string{fmt.Sprintf("[%s] %s", ts, msg)}, m.eventLog...)
	if len(m.eventLog) > 100 {
		m.eventLog = m.eventLog[:100]
	}
}

// GetEventLog returns a copy of the event log (newest first)
func (m *MonitorService) GetEventLog() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.eventLog))
	copy(out, m.eventLog)
	return out
}

// GetTimers returns the current high/low sustained seconds for a LB without mutating state.
// For read-only UI display.
func (m *MonitorService) GetTimers(lbName string) (highSec, lowSec int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	if t, ok := m.highSince[lbName]; ok {
		highSec = int(now.Sub(t).Seconds())
	}
	if t, ok := m.lowSince[lbName]; ok {
		lowSec = int(now.Sub(t).Seconds())
	}
	return
}

// ComputeMaxAvg returns max and avg CPU across valid metrics (pure calculation, no state)
func (m *MonitorService) ComputeMaxAvg(metrics []models.CPUMetric) (maxCPU, avgCPU float64) {
	var sum float64
	var count int
	for _, mt := range metrics {
		if mt.Error != "" {
			continue
		}
		if mt.CPUPercent > maxCPU {
			maxCPU = mt.CPUPercent
		}
		sum += mt.CPUPercent
		count++
	}
	if count > 0 {
		avgCPU = sum / float64(count)
	}
	return
}

// EvaluateThresholds returns:
//   action: "up" | "down" | ""
//   highSec, lowSec: seconds sustained
func (m *MonitorService) EvaluateThresholds(lbName string, metrics []models.CPUMetric, cfg *models.ElasticityConfig) (action string, highSec int, lowSec int, maxCPU float64, avgCPU float64) {
	if cfg == nil || !cfg.Enabled {
		return "", 0, 0, 0, 0
	}

	var sum float64
	var count int
	for _, mt := range metrics {
		if mt.Error != "" {
			continue
		}
		if mt.CPUPercent > maxCPU {
			maxCPU = mt.CPUPercent
		}
		sum += mt.CPUPercent
		count++
	}
	if count > 0 {
		avgCPU = sum / float64(count)
	}

	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	highActive := maxCPU >= cfg.UpperThresholdCPU && count > 0
	lowActive := maxCPU <= cfg.LowerThresholdCPU && count > 0

	if highActive {
		if _, ok := m.highSince[lbName]; !ok {
			m.highSince[lbName] = now
		}
	} else {
		delete(m.highSince, lbName)
	}
	if lowActive {
		if _, ok := m.lowSince[lbName]; !ok {
			m.lowSince[lbName] = now
		}
	} else {
		delete(m.lowSince, lbName)
	}

	if t, ok := m.highSince[lbName]; ok {
		highSec = int(now.Sub(t).Seconds())
	}
	if t, ok := m.lowSince[lbName]; ok {
		lowSec = int(now.Sub(t).Seconds())
	}

	// Cooldown enforcement
	if cfg.CooldownSeconds > 0 && !cfg.LastScaleAction.IsZero() {
		if int(now.Sub(cfg.LastScaleAction).Seconds()) < cfg.CooldownSeconds {
			return "", highSec, lowSec, maxCPU, avgCPU
		}
	}

	if highSec >= cfg.SustainedSeconds && cfg.SustainedSeconds > 0 {
		return "up", highSec, lowSec, maxCPU, avgCPU
	}
	if lowSec >= cfg.SustainedSeconds && cfg.SustainedSeconds > 0 {
		return "down", highSec, lowSec, maxCPU, avgCPU
	}
	return "", highSec, lowSec, maxCPU, avgCPU
}

// ResetLBTimers clears timers after a scale action
func (m *MonitorService) ResetLBTimers(lbName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.highSince, lbName)
	delete(m.lowSince, lbName)
}

// StartStress runs stress-ng on a VM for load simulation (installs it if missing)
func (m *MonitorService) StartStress(vmName, username string, rootPassword string, cpus, loadPercent, duration int) error {
	port, err := m.VBox.GetNATPort(vmName)
	if err != nil || port == "" {
		return fmt.Errorf("sin puerto SSH para VM '%s'", vmName)
	}
	rootKey := m.SSHSvc.GetPrivateKeyPath(vmName, "root")

	// Install stress-ng if missing
	install := `if ! command -v stress-ng >/dev/null 2>&1; then export DEBIAN_FRONTEND=noninteractive; apt-get update -y >/dev/null 2>&1 && apt-get install -y stress-ng >/dev/null 2>&1; fi; command -v stress-ng`
	if err := runSSHCommandWithKeyOrPassword(port, "root", rootKey, rootPassword, install); err != nil {
		return fmt.Errorf("no se pudo instalar stress-ng: %v", err)
	}

	if cpus <= 0 {
		cpus = 1
	}
	if loadPercent <= 0 {
		loadPercent = 80
	}
	if duration <= 0 {
		duration = 120
	}

	// Run in background so SSH returns immediately; kill any previous instance
	cmd := fmt.Sprintf("pkill stress-ng >/dev/null 2>&1 || true; nohup stress-ng --cpu %d --cpu-load %d --timeout %ds </dev/null >/tmp/stress.log 2>&1 &",
		cpus, loadPercent, duration)
	if err := runSSHCommandWithKeyOrPassword(port, "root", rootKey, rootPassword, cmd); err != nil {
		return fmt.Errorf("error al iniciar stress-ng: %v", err)
	}
	m.LogEvent(fmt.Sprintf("stress-ng iniciado en %s: cpus=%d load=%d%% tiempo=%ds", vmName, cpus, loadPercent, duration))
	return nil
}

// StopStress kills stress-ng on a VM
func (m *MonitorService) StopStress(vmName string, rootPassword string) error {
	port, err := m.VBox.GetNATPort(vmName)
	if err != nil || port == "" {
		return fmt.Errorf("sin puerto SSH para VM '%s'", vmName)
	}
	rootKey := m.SSHSvc.GetPrivateKeyPath(vmName, "root")
	_ = runSSHCommandWithKeyOrPassword(port, "root", rootKey, rootPassword, "pkill stress-ng || true")
	m.LogEvent(fmt.Sprintf("stress-ng detenido en %s", vmName))
	return nil
}
