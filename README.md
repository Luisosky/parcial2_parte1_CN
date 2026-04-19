# VM Platform — Parcial 2 (Parte 1, 2 & 3)

Plataforma web desarrollada en **Go** para automatizar la gestión de máquinas virtuales en Oracle VirtualBox, despliegue de aplicaciones, gestión de servicios systemd y **elasticidad automática con HAProxy**.

---

## Requisitos Previos

- **Go** 1.21 o superior → [https://go.dev/dl/](https://go.dev/dl/)
- **Oracle VirtualBox** con `VBoxManage` accesible en el PATH
- Al menos **2 máquinas virtuales** con Debian 13 (CLI) configuradas en VirtualBox
- **Servidor SSH** instalado y configurado en cada VM
- Las VMs deben tener acceso SSH con llaves de autenticación configuradas
- Para la Parte 3: **una VM adicional dedicada para HAProxy** (Debian CLI con acceso a internet para `apt-get install haproxy`)

---

## Estructura del Proyecto

```
vm-platform/
├── main.go                               # Entry point
├── go.mod / go.sum
├── handlers/
│   ├── handlers.go                       # Parte 1 — VMs
│   ├── deploy_handlers.go                # Parte 2 — Deploy/Services
│   └── elasticity_handlers.go            # Parte 3 — Elasticidad (NUEVO)
├── models/
│   └── models.go                         # Incluye tipos de Parte 3 (NUEVO)
├── services/
│   ├── vbox_service.go                   # VBoxManage
│   ├── ssh_service.go                    # SSH (llaves RSA)
│   ├── platform_service.go               # Parte 1
│   ├── deploy_service.go                 # Parte 2
│   ├── haproxy_service.go                # Parte 3 — HAProxy mgmt (NUEVO)
│   ├── monitor_service.go                # Parte 3 — CPU sampling (NUEVO)
│   └── elasticity_service.go             # Parte 3 — Auto-scaling loop (NUEVO)
├── sample-app/                           # App de ejemplo para Parte 2
├── templates/
│   └── index.html                        # Dashboard (3 pestañas)
└── static/
```

---

## Ejecución

```bash
go build -o vm-platform .
./vm-platform
```

Servidor en **http://localhost:8080**

---

## Parte 3 — Elasticidad Automática

Implementa elasticidad automática usando HAProxy como balanceador de carga y VMs VirtualBox como backends que se crean/destruyen dinámicamente según el consumo de CPU.

### Arquitectura

```
┌─────────────────────┐
│  VM Platform (Go)   │   ← Dashboard web, loop de elasticidad
└──────────┬──────────┘
           │ SSH + VBoxManage
   ┌───────┴────────┐
   ▼                ▼
┌─────────┐   ┌──────────────────┐
│ VM LB   │   │ VM Backends       │
│ HAProxy │──▶│ App + CPU monitor │
│ :80     │   │ :8080             │
└─────────┘   └──────────────────┘
                     ▲ escalado
                     │ desde disco
                     │ multiattach
                     ▼
              ┌──────────────┐
              │ Disco .vdi   │
              │ multiattach  │
              └──────────────┘
```

### Flujo de uso (paso a paso)

1. **Preparación (Parte 1):**
   - Crea una VM base con Debian + tu app pre-instalada.
   - Despliega llaves root SSH en ella.
   - Convierte su disco a **multiattach**.
   - Crea una VM de usuario desde el disco (para tener al menos 1 backend inicial).
   - Crea una VM adicional para HAProxy (también necesita llaves root SSH).

2. **Crea el balanceador** (pestaña Parte 3):
   - Nombre del LB, selecciona VM dedicada para HAProxy.
   - Puerto frontend (ej: 80) y puerto backend (ej: 8080).
   - Contraseña root para instalar HAProxy.
   - Clic en "Instalar" — esto hace `apt-get install haproxy` via SSH y configura port-forwarding NAT.

3. **Agrega backends manualmente:**
   - Al menos 1 backend inicial apuntando a tu app.
   - Dirección: `10.0.2.2` (NAT host visto desde VM HAProxy) + puerto forwardeado.

4. **Configura elasticidad:**
   - Umbral superior CPU (ej: 75%) — se escala hacia arriba si es superado por `sustained_seconds`.
   - Umbral inferior CPU (ej: 20%) — se escala hacia abajo si es respetado.
   - Disco multiattach base — desde donde se clonarán nuevas VMs.
   - Marca "Elasticidad activa".

5. **Prueba con stress-ng:**
   - Haz clic en el ícono de stress-ng junto a un backend.
   - Configura carga (ej: 1 CPU al 90% por 180s).
   - Observa cómo la CPU sube, el timer llega a `sustained_seconds`, y se crea una nueva VM automáticamente.

### Componentes técnicos

#### HAProxy (`haproxy_service.go`)
- Instala HAProxy via `apt-get` por SSH.
- Genera `/etc/haproxy/haproxy.cfg` con frontend/backend dinámicos.
- Valida configuración con `haproxy -c` antes de recargar.
- Expone stats en `:9000` con auth `admin/admin`.
- Configura automáticamente port-forwarding NAT en la VM del LB.

#### Monitor de CPU (`monitor_service.go`)
- Muestrea CPU vía SSH usando diff de `/proc/stat` (más preciso que `top`).
- Mantiene historial de 60 muestras por VM.
- Evalúa umbrales con timers sostenidos (inicia contador cuando se cruza el umbral, se resetea al descenso).
- Respeta cooldown entre acciones consecutivas.
- Ejecuta `stress-ng` con instalación automática.

#### Orquestador (`elasticity_service.go`)
- Loop de fondo con heartbeat de 5s, cada LB muestrea a su `sample_interval_sec` configurado.
- **Scale-up:** crea nueva VM con `platform.CreateUserVM(name, ..., baseDisk)`, arranca la VM, agrega port-forward NAT para la app (host → guest), registra el backend en HAProxy con `10.0.2.2:<host_port>`.
- **Scale-down:** elimina el backend más recientemente auto-escalado, recarga HAProxy, apaga y elimina la VM.
- Persiste estado en `~/.vm-platform/loadbalancers.json`.

### API REST (Parte 3)

| Método | Endpoint | Descripción |
|--------|----------|-------------|
| GET | `/api/lb/list` | Listar balanceadores |
| POST | `/api/lb/create` | Crear balanceador |
| POST | `/api/lb/update` | Actualizar LB |
| POST | `/api/lb/delete` | Eliminar LB |
| POST | `/api/lb/install` | Instalar HAProxy y aplicar config |
| POST | `/api/lb/action` | start/stop/restart HAProxy |
| GET | `/api/lb/status` | Estado de HAProxy |
| POST | `/api/lb/backend/add` | Agregar backend |
| POST | `/api/lb/backend/update` | Actualizar backend |
| POST | `/api/lb/backend/remove` | Remover backend |
| POST | `/api/lb/backend/toggle` | Habilitar/deshabilitar backend |
| POST | `/api/lb/elasticity/update` | Actualizar config de elasticidad |
| GET | `/api/lb/snapshot` | Snapshot en vivo (métricas + backends + eventos) |
| GET | `/api/lb/events` | Log de eventos de elasticidad |
| GET | `/api/lb/cpu-history` | Historial CPU de una VM |
| POST | `/api/stress/start` | Iniciar stress-ng en una VM |
| POST | `/api/stress/stop` | Detener stress-ng |
| GET | `/api/lb/available-vms` | VMs candidatas para backend |
| GET | `/api/lb/disks` | Discos multiattach disponibles |

---

## Problemas Encontrados

### Parte 1
- El tiempo de espera al iniciar VMs puede variar según recursos del host.
- Los discos multiattach de VirtualBox requieren secuencia exacta de desconexión/conversión.

### Parte 2
- Transferencia de archivos grandes por SSH pipe puede ser lenta.

### Parte 3
- VirtualBox usa NAT por defecto para cada VM, lo que hace que las VMs no se vean entre sí. La solución fue usar `10.0.2.2:<host_port>` con port-forwarding como vía de comunicación entre la VM HAProxy y los backends.
- `apt-get install haproxy` en VMs sin caché previa toma 30-90s.
- Medir CPU vía `top -bn1` da lecturas inestables; cambiamos a diff de `/proc/stat` con sleep 1s.
- Primera muestra de CPU después de iniciar una VM nueva puede ser errática durante los primeros ~20s.

---

## Propuestas de Mejora (Parte 3)

1. Gráficas históricas de CPU con Chart.js en el dashboard.
2. Scale-up predictivo basado en tendencia (no solo umbral instantáneo).
3. Soporte para múltiples algoritmos de scale-down (más reciente, menos cargado, etc.).
4. Health checks personalizados HTTP en lugar de TCP-check por defecto.
5. Métricas adicionales: RAM, disco, conexiones activas en HAProxy.
6. Red "hostonly" en vez de NAT para comunicación directa entre VMs (requiere configuración VirtualBox adicional).
7. Soporte para múltiples LBs en la misma VM.
8. Notificaciones (webhook/email) en eventos de escalado.

---

## Conocimientos Aprendidos (Parte 3)

- Instalación y configuración programática de HAProxy vía SSH.
- Diseño de loop de monitoreo concurrente con `sync.WaitGroup` en Go.
- Medición precisa de CPU en Linux usando `/proc/stat` con diferencial temporal.
- Diseño de máquina de estados de elasticidad con timers sostenidos + cooldown.
- Port-forwarding NAT dinámico en VirtualBox con `VBoxManage modifyvm --natpf1`.
- Generación de configuración HAProxy programáticamente con validación previa (`haproxy -c`).
- Comunicación entre VMs NAT mediante el host (`10.0.2.2`).
- Patrón orquestador-servicio en Go: separación clara entre HAProxy ops, monitoring y lógica de scaling.
- Sincronización de estado persistente (JSON) con acceso concurrente (`sync.RWMutex`).
- Simulación de carga CPU controlable con `stress-ng --cpu N --cpu-load M --timeout T`.

---

## Notas de uso

- **Primera vez:** después de crear un LB, haz clic en "Instalar" antes de agregar backends.
- **Configuración recomendada para pruebas:**
  - Umbral superior: 75%, inferior: 20%
  - Tiempo sostenido: 60s
  - Intervalo muestreo: 10s
  - Cooldown: 60s
  - Min/Max backends: 1 / 4
- **Test rápido:** usa `sustained_seconds=30` para ver el escalado más rápido durante demos.
- **Stats HAProxy:** después de instalar, abre `http://<host>:9000` (usuario/pass: admin/admin).
