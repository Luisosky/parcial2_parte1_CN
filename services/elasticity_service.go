package services

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vm-platform/models"
)

// ElasticityService orchestrates load balancer lifecycle, backend management,
// and the auto-scaling loop (monitor -> evaluate -> scale up/down).
type ElasticityService struct {
	VBox     *VBoxService
	SSHSvc   *SSHService
	HAProxy  *HAProxyService
	Monitor  *MonitorService
	Platform *PlatformService

	mu            sync.RWMutex
	LoadBalancers map[string]*models.LoadBalancer
	dataFile      string

	// control
	stopChan  chan struct{}
	running   bool
	loopReady sync.Once
}

func NewElasticityService(platform *PlatformService, haproxy *HAProxyService, monitor *MonitorService) *ElasticityService {
	usr, _ := user.Current()
	dataDir := filepath.Join(usr.HomeDir, ".vm-platform")
	os.MkdirAll(dataDir, 0755)

	es := &ElasticityService{
		VBox:          platform.VBox,
		SSHSvc:        platform.SSH,
		HAProxy:       haproxy,
		Monitor:       monitor,
		Platform:      platform,
		LoadBalancers: make(map[string]*models.LoadBalancer),
		dataFile:      filepath.Join(dataDir, "loadbalancers.json"),
		stopChan:      make(chan struct{}),
	}
	es.loadState()
	return es
}

func (e *ElasticityService) saveState() {
	jd, _ := json.MarshalIndent(e.LoadBalancers, "", "  ")
	os.WriteFile(e.dataFile, jd, 0644)
}

func (e *ElasticityService) loadState() {
	data, err := os.ReadFile(e.dataFile)
	if err != nil {
		return
	}
	json.Unmarshal(data, &e.LoadBalancers)
}

// ─── Load Balancer CRUD ───

func (e *ElasticityService) CreateLoadBalancer(req *models.LoadBalancerCreateRequest) (*models.LoadBalancer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.LoadBalancers[req.Name]; ok {
		return nil, fmt.Errorf("ya existe un balanceador llamado '%s'", req.Name)
	}
	if req.Name == "" || req.VMName == "" {
		return nil, fmt.Errorf("nombre y VM son requeridos")
	}
	if req.FrontendPort <= 0 {
		req.FrontendPort = 80
	}
	if req.BackendPort <= 0 {
		req.BackendPort = 8080
	}
	if req.Algorithm == "" {
		req.Algorithm = "roundrobin"
	}
	if req.Username == "" {
		req.Username = "root"
	}

	lb := &models.LoadBalancer{
		Name:         req.Name,
		VMName:       req.VMName,
		Username:     req.Username,
		RootPassword: req.RootPassword,
		FrontendPort: req.FrontendPort,
		BackendPort:  req.BackendPort,
		Algorithm:    req.Algorithm,
		Backends:     []*models.BackendServer{},
		Elasticity: &models.ElasticityConfig{
			Enabled:           false,
			UpperThresholdCPU: 75,
			LowerThresholdCPU: 20,
			SustainedSeconds:  60,
			SampleIntervalSec: 10,
			MinBackends:       1,
			MaxBackends:       4,
			CooldownSeconds:   60,
			NewVMNamePrefix:   "auto-backend-",
			AppPort:           req.BackendPort,
		},
		CreatedAt: time.Now(),
	}
	e.LoadBalancers[lb.Name] = lb
	e.saveState()
	e.Monitor.LogEvent(fmt.Sprintf("LB '%s' creado sobre VM '%s' (frontend %d)", lb.Name, lb.VMName, lb.FrontendPort))
	return lb, nil
}

func (e *ElasticityService) UpdateLoadBalancer(name string, updates *models.LoadBalancerCreateRequest) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	lb, ok := e.LoadBalancers[name]
	if !ok {
		return fmt.Errorf("balanceador '%s' no encontrado", name)
	}
	if updates.FrontendPort > 0 {
		lb.FrontendPort = updates.FrontendPort
	}
	if updates.BackendPort > 0 {
		lb.BackendPort = updates.BackendPort
	}
	if updates.Algorithm != "" {
		lb.Algorithm = updates.Algorithm
	}
	if updates.RootPassword != "" {
		lb.RootPassword = updates.RootPassword
	}
	e.saveState()
	return nil
}

func (e *ElasticityService) DeleteLoadBalancer(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	lb, ok := e.LoadBalancers[name]
	if !ok {
		return fmt.Errorf("balanceador no encontrado")
	}
	// Attempt to stop HAProxy (best-effort)
	_ = e.HAProxy.Action(lb, "stop")
	delete(e.LoadBalancers, name)
	e.saveState()
	e.Monitor.LogEvent(fmt.Sprintf("LB '%s' eliminado", name))
	return nil
}

func (e *ElasticityService) GetLoadBalancer(name string) *models.LoadBalancer {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.LoadBalancers[name]
}

func (e *ElasticityService) ListLoadBalancers() []*models.LoadBalancer {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*models.LoadBalancer, 0, len(e.LoadBalancers))
	for _, lb := range e.LoadBalancers {
		out = append(out, lb)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// InstallAndStart installs HAProxy (if needed) and applies the current config
func (e *ElasticityService) InstallAndStart(name string) error {
	e.mu.Lock()
	lb, ok := e.LoadBalancers[name]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("LB no encontrado")
	}
	if err := e.HAProxy.Install(lb); err != nil {
		return err
	}
	if err := e.HAProxy.ApplyConfig(lb); err != nil {
		return err
	}
	e.saveState()
	e.Monitor.LogEvent(fmt.Sprintf("HAProxy instalado y corriendo en LB '%s'", lb.Name))
	return nil
}

// ─── Backend CRUD ───

func (e *ElasticityService) AddBackend(req *models.BackendAddRequest) error {
	e.mu.Lock()
	lb, ok := e.LoadBalancers[req.LBName]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("LB '%s' no encontrado", req.LBName)
	}
	// Dup check
	for _, b := range lb.Backends {
		if b.Name == req.Name {
			e.mu.Unlock()
			return fmt.Errorf("ya existe un backend con nombre '%s'", req.Name)
		}
	}
	port := req.Port
	if port <= 0 {
		port = lb.BackendPort
	}
	weight := req.Weight
	if weight <= 0 {
		weight = 1
	}
	addr := req.Address
	if addr == "" {
		addr = "127.0.0.1"
	}
	b := &models.BackendServer{
		Name:     req.Name,
		VMName:   req.VMName,
		Username: req.Username,
		Address:  addr,
		Port:     port,
		Weight:   weight,
		Enabled:  true,
		AddedAt:  time.Now(),
	}
	lb.Backends = append(lb.Backends, b)
	e.saveState()
	e.mu.Unlock()

	if err := e.HAProxy.ApplyConfig(lb); err != nil {
		return fmt.Errorf("backend agregado pero falló recarga HAProxy: %v", err)
	}
	e.Monitor.LogEvent(fmt.Sprintf("Backend '%s' agregado al LB '%s' (%s:%d)", b.Name, lb.Name, b.Address, b.Port))
	return nil
}

func (e *ElasticityService) UpdateBackend(lbName, backendName string, req *models.BackendAddRequest) error {
	e.mu.Lock()
	lb, ok := e.LoadBalancers[lbName]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("LB no encontrado")
	}
	var target *models.BackendServer
	for _, b := range lb.Backends {
		if b.Name == backendName {
			target = b
			break
		}
	}
	if target == nil {
		e.mu.Unlock()
		return fmt.Errorf("backend no encontrado")
	}
	if req.Address != "" {
		target.Address = req.Address
	}
	if req.Port > 0 {
		target.Port = req.Port
	}
	if req.Weight > 0 {
		target.Weight = req.Weight
	}
	e.saveState()
	e.mu.Unlock()

	return e.HAProxy.ApplyConfig(lb)
}

func (e *ElasticityService) RemoveBackend(lbName, backendName string) error {
	e.mu.Lock()
	lb, ok := e.LoadBalancers[lbName]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("LB no encontrado")
	}
	var removed *models.BackendServer
	newList := make([]*models.BackendServer, 0, len(lb.Backends))
	for _, b := range lb.Backends {
		if b.Name == backendName {
			removed = b
			continue
		}
		newList = append(newList, b)
	}
	if removed == nil {
		e.mu.Unlock()
		return fmt.Errorf("backend no encontrado")
	}
	lb.Backends = newList
	e.saveState()
	e.mu.Unlock()

	// Reload HAProxy
	if err := e.HAProxy.ApplyConfig(lb); err != nil {
		return fmt.Errorf("backend removido pero falló recarga: %v", err)
	}

	// If this was an auto-scaled backend, tear down its VM
	if removed.AutoScaled && removed.VMName != "" {
		go e.tearDownAutoScaledVM(removed)
	}
	e.Monitor.LogEvent(fmt.Sprintf("Backend '%s' removido del LB '%s'", backendName, lbName))
	return nil
}

// ToggleBackend enables/disables a backend in HAProxy without deleting it
func (e *ElasticityService) ToggleBackend(lbName, backendName string, enabled bool) error {
	e.mu.Lock()
	lb, ok := e.LoadBalancers[lbName]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("LB no encontrado")
	}
	var target *models.BackendServer
	for _, b := range lb.Backends {
		if b.Name == backendName {
			target = b
			break
		}
	}
	if target == nil {
		e.mu.Unlock()
		return fmt.Errorf("backend no encontrado")
	}
	target.Enabled = enabled
	e.saveState()
	e.mu.Unlock()
	return e.HAProxy.ApplyConfig(lb)
}

// ─── Elasticity Config ───

func (e *ElasticityService) UpdateElasticity(req *models.ElasticityUpdateRequest) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	lb, ok := e.LoadBalancers[req.LBName]
	if !ok {
		return fmt.Errorf("LB no encontrado")
	}
	if lb.Elasticity == nil {
		lb.Elasticity = &models.ElasticityConfig{}
	}
	lb.Elasticity.Enabled = req.Enabled
	if req.UpperThresholdCPU > 0 {
		lb.Elasticity.UpperThresholdCPU = req.UpperThresholdCPU
	}
	if req.LowerThresholdCPU >= 0 {
		lb.Elasticity.LowerThresholdCPU = req.LowerThresholdCPU
	}
	if req.SustainedSeconds > 0 {
		lb.Elasticity.SustainedSeconds = req.SustainedSeconds
	}
	if req.SampleIntervalSec > 0 {
		lb.Elasticity.SampleIntervalSec = req.SampleIntervalSec
	}
	if req.MinBackends > 0 {
		lb.Elasticity.MinBackends = req.MinBackends
	}
	if req.MaxBackends > 0 {
		lb.Elasticity.MaxBackends = req.MaxBackends
	}
	if req.CooldownSeconds >= 0 {
		lb.Elasticity.CooldownSeconds = req.CooldownSeconds
	}
	if req.BaseDiskName != "" {
		lb.Elasticity.BaseDiskName = req.BaseDiskName
	}
	if req.NewVMNamePrefix != "" {
		lb.Elasticity.NewVMNamePrefix = req.NewVMNamePrefix
	}
	if req.NewVMUser != "" {
		lb.Elasticity.NewVMUser = req.NewVMUser
	}
	if req.AppExecCommand != "" {
		lb.Elasticity.AppExecCommand = req.AppExecCommand
	}
	if req.AppPort > 0 {
		lb.Elasticity.AppPort = req.AppPort
	}
	e.saveState()
	diskInfo := lb.Elasticity.BaseDiskName
	if diskInfo == "" {
		diskInfo = "SIN DISCO"
	}
	e.Monitor.LogEvent(fmt.Sprintf("[%s] Elasticidad actualizada: enabled=%v, up=%.0f%%, down=%.0f%%, sustained=%ds, disco=%s",
		lb.Name, lb.Elasticity.Enabled, lb.Elasticity.UpperThresholdCPU, lb.Elasticity.LowerThresholdCPU, lb.Elasticity.SustainedSeconds, diskInfo))
	if lb.Elasticity.Enabled && lb.Elasticity.BaseDiskName == "" {
		e.Monitor.LogEvent(fmt.Sprintf("[%s] ADVERTENCIA: elasticidad activa pero SIN disco configurado — el scale-up fallará", lb.Name))
	}
	return nil
}

// ─── Snapshot / Metrics ───

func (e *ElasticityService) GetSnapshot(lbName string) *models.MetricsSnapshot {
	e.mu.RLock()
	lb, ok := e.LoadBalancers[lbName]
	e.mu.RUnlock()
	if !ok {
		return nil
	}
	var vmNames []string
	for _, b := range lb.Backends {
		vmNames = append(vmNames, b.VMName)
	}
	metrics := e.Monitor.GetLatest(vmNames)
	// Filter event log to only show entries relevant to this LB
	allEvents := e.Monitor.GetEventLog()
	var filteredEvents []string
	for _, ev := range allEvents {
		if strings.Contains(ev, "'"+lbName+"'") || strings.Contains(ev, "["+lbName+"]") {
			filteredEvents = append(filteredEvents, ev)
		}
	}
	if len(filteredEvents) == 0 {
		filteredEvents = []string{}
	}

	snap := &models.MetricsSnapshot{
		LBName:          lbName,
		Backends:        lb.Backends,
		Metrics:         metrics,
		CurrentBackends: len(lb.Backends),
		Enabled:         lb.Elasticity != nil && lb.Elasticity.Enabled,
		EventLog:        filteredEvents,
	}
	// Recompute timers (read-only, no state mutation)
	if lb.Elasticity != nil {
		maxc, avgc := e.Monitor.ComputeMaxAvg(metrics)
		high, low := e.Monitor.GetTimers(lbName)
		snap.HighSinceSeconds = high
		snap.LowSinceSeconds = low
		snap.MaxCPUPercent = maxc
		snap.AvgCPUPercent = avgc
		snap.LastAction = lb.Elasticity.LastScaleType
		snap.LastActionTime = lb.Elasticity.LastScaleAction
	}
	return snap
}

// ─── Auto-scaling loop ───

func (e *ElasticityService) Start() {
	e.loopReady.Do(func() {
		e.running = true
		go e.loop()
	})
}

func (e *ElasticityService) Stop() {
	if e.running {
		close(e.stopChan)
		e.running = false
	}
}

func (e *ElasticityService) loop() {
	// Use a 5s heartbeat; each LB samples at its configured interval.
	lastSample := make(map[string]time.Time)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopChan:
			return
		case now := <-ticker.C:
			e.mu.RLock()
			lbs := make([]*models.LoadBalancer, 0, len(e.LoadBalancers))
			for _, lb := range e.LoadBalancers {
				lbs = append(lbs, lb)
			}
			e.mu.RUnlock()

			for _, lb := range lbs {
				if lb.Elasticity == nil {
					continue
				}
				interval := lb.Elasticity.SampleIntervalSec
				if interval <= 0 {
					interval = 10
				}
				if t, ok := lastSample[lb.Name]; ok {
					if now.Sub(t).Seconds() < float64(interval) {
						continue
					}
				}
				lastSample[lb.Name] = now
				e.tickLB(lb)
			}
		}
	}
}

func (e *ElasticityService) tickLB(lb *models.LoadBalancer) {
	// 1. Sample every backend concurrently
	var wg sync.WaitGroup
	metricsCh := make(chan models.CPUMetric, len(lb.Backends))
	for _, b := range lb.Backends {
		if b == nil {
			continue
		}
		wg.Add(1)
		go func(be *models.BackendServer) {
			defer wg.Done()
			metricsCh <- e.Monitor.SampleCPU(be.VMName, be.Username, lb.RootPassword)
		}(b)
	}
	wg.Wait()
	close(metricsCh)

	var metrics []models.CPUMetric
	for m := range metricsCh {
		metrics = append(metrics, m)
		e.Monitor.RecordMetric(m)
	}

	// 2. Evaluate thresholds (only if elasticity is enabled)
	if lb.Elasticity == nil || !lb.Elasticity.Enabled {
		return
	}
	action, _, _, maxCPU, _ := e.Monitor.EvaluateThresholds(lb.Name, metrics, lb.Elasticity)
	if action == "" {
		return
	}

	// 3. Execute action
	switch action {
	case "up":
		if len(lb.Backends) >= lb.Elasticity.MaxBackends {
			return
		}
		if lb.Elasticity.BaseDiskName == "" {
			// Evita spam: log una vez y resetea timers para no reintentar cada tick.
			e.Monitor.LogEvent(fmt.Sprintf("[%s] scale-up pospuesto: no hay disco multiattach configurado (max=%.1f%%)", lb.Name, maxCPU))
			e.Monitor.ResetLBTimers(lb.Name)
			return
		}
		e.Monitor.LogEvent(fmt.Sprintf("[%s] CPU alta sostenida (max=%.1f%%) -> escalar UP", lb.Name, maxCPU))
		if err := e.scaleUp(lb); err != nil {
			e.Monitor.LogEvent(fmt.Sprintf("[%s] ERROR scale-up: %v", lb.Name, err))
			e.Monitor.ResetLBTimers(lb.Name)
			return
		}
		lb.Elasticity.LastScaleAction = time.Now()
		lb.Elasticity.LastScaleType = "up"
		e.Monitor.ResetLBTimers(lb.Name)
		e.saveState()

	case "down":
		if len(lb.Backends) <= lb.Elasticity.MinBackends {
			return
		}
		e.Monitor.LogEvent(fmt.Sprintf("[%s] CPU baja sostenida (max=%.1f%%) -> escalar DOWN", lb.Name, maxCPU))
		if err := e.scaleDown(lb); err != nil {
			e.Monitor.LogEvent(fmt.Sprintf("[%s] ERROR scale-down: %v", lb.Name, err))
			return
		}
		lb.Elasticity.LastScaleAction = time.Now()
		lb.Elasticity.LastScaleType = "down"
		e.Monitor.ResetLBTimers(lb.Name)
		e.saveState()
	}
}

// scaleUp provisions a new VM from the configured multiattach disk and adds it as backend
func (e *ElasticityService) scaleUp(lb *models.LoadBalancer) error {
	cfg := lb.Elasticity
	if cfg.BaseDiskName == "" {
		return fmt.Errorf("no hay disco multiattach configurado para escalar")
	}

	// Generate unique VM name
	prefix := cfg.NewVMNamePrefix
	if prefix == "" {
		prefix = "auto-backend-"
	}
	vmName := fmt.Sprintf("%s%d", prefix, time.Now().Unix())

	// Use PlatformService to create the VM from the multiattach disk
	_, err := e.Platform.CreateUserVM(vmName, "Auto-escalada por elasticidad", cfg.BaseDiskName)
	if err != nil {
		return fmt.Errorf("error al crear VM: %v", err)
	}

	// Start the VM so the app comes up (multiattach disk already has user + app)
	if err := e.VBox.StartVM(vmName); err != nil {
		return fmt.Errorf("error al iniciar VM: %v", err)
	}
	// Wait for SSH to be reachable
	time.Sleep(35 * time.Second)

	// Determine backend address: reach from HAProxy VM. Using 10.0.2.2 NAT trick
	// isn't reliable across VMs. We use the host's NAT-forwarded port on 10.0.2.2
	// from inside the LB VM — but since each user VM has its own NAT, the
	// practical approach is: HAProxy VM connects to host (10.0.2.2) at the
	// backend VM's forwarded port. We use port = the new VM's SSH NAT port +
	// app_port offset isn't clean either.
	//
	// Cleanest path: forward the app port on the new VM (guest -> host) and
	// have HAProxy VM connect to 10.0.2.2:<forwarded host port>.
	hostAppPort := e.allocateHostPort()
	appPort := cfg.AppPort
	if appPort <= 0 {
		appPort = lb.BackendPort
	}
	rule := fmt.Sprintf("app,tcp,,%d,,%d", hostAppPort, appPort)
	_, _ = e.VBox.runCommand("modifyvm", vmName, "--natpf1", "delete", "app")
	_, _ = e.VBox.runCommand("modifyvm", vmName, "--natpf1", rule)

	// Add backend pointing to 10.0.2.2:hostAppPort (host seen from LB VM's NAT)
	backend := &models.BackendServer{
		Name:       vmName,
		VMName:     vmName,
		Username:   cfg.NewVMUser,
		Address:    "10.0.2.2",
		Port:       hostAppPort,
		Weight:     1,
		Enabled:    true,
		AutoScaled: true,
		DiskName:   cfg.BaseDiskName,
		AddedAt:    time.Now(),
	}
	e.mu.Lock()
	lb.Backends = append(lb.Backends, backend)
	e.saveState()
	e.mu.Unlock()

	if err := e.HAProxy.ApplyConfig(lb); err != nil {
		return fmt.Errorf("error al recargar HAProxy: %v", err)
	}
	e.Monitor.LogEvent(fmt.Sprintf("[%s] ✓ Nueva VM '%s' agregada (puerto host %d -> guest %d)", lb.Name, vmName, hostAppPort, appPort))
	return nil
}

// scaleDown removes the most recent auto-scaled backend and powers off the VM
func (e *ElasticityService) scaleDown(lb *models.LoadBalancer) error {
	// Find newest auto-scaled backend
	var target *models.BackendServer
	var targetIdx int = -1
	for i := len(lb.Backends) - 1; i >= 0; i-- {
		if lb.Backends[i].AutoScaled {
			target = lb.Backends[i]
			targetIdx = i
			break
		}
	}
	if target == nil {
		return fmt.Errorf("no hay backends auto-escalados para eliminar")
	}

	lb.Backends = append(lb.Backends[:targetIdx], lb.Backends[targetIdx+1:]...)
	e.saveState()

	if err := e.HAProxy.ApplyConfig(lb); err != nil {
		return fmt.Errorf("error al recargar HAProxy: %v", err)
	}

	go e.tearDownAutoScaledVM(target)
	e.Monitor.LogEvent(fmt.Sprintf("[%s] ✓ VM '%s' removida y apagada", lb.Name, target.VMName))
	return nil
}

func (e *ElasticityService) tearDownAutoScaledVM(b *models.BackendServer) {
	// Stop + unregister + delete
	_ = e.VBox.StopVM(b.VMName)
	time.Sleep(3 * time.Second)
	_, _ = e.VBox.runCommand("unregistervm", b.VMName, "--delete")

	// Remove from UserVMs registry too
	e.Platform.mu.Lock()
	delete(e.Platform.UserVMs, b.VMName)
	e.Platform.saveState()
	e.Platform.mu.Unlock()
}

// allocateHostPort picks a free host port for NAT forwarding of auto-scaled VM apps
var hostPortCounter = 18080

func (e *ElasticityService) allocateHostPort() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	used := map[int]bool{}
	for _, lb := range e.LoadBalancers {
		for _, b := range lb.Backends {
			if strings.HasPrefix(b.Address, "10.0.2.2") {
				used[b.Port] = true
			}
		}
	}
	for hostPortCounter < 19000 {
		if !used[hostPortCounter] {
			p := hostPortCounter
			hostPortCounter++
			return p
		}
		hostPortCounter++
	}
	hostPortCounter = 18080
	return hostPortCounter
}
