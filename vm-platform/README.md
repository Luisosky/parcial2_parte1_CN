# VM Platform — Plataforma Web de Gestión de Máquinas Virtuales

Plataforma web desarrollada en **Go (Golang)** para automatizar la gestión de máquinas virtuales en Oracle VirtualBox, incluyendo acceso remoto SSH con llaves de autenticación RSA.

---

## Requisitos Previos

- **Go** 1.21 o superior → [https://go.dev/dl/](https://go.dev/dl/)
- **Oracle VirtualBox** con `VBoxManage` accesible en el PATH
- **sshpass** instalado en el sistema host
- Al menos **2 máquinas virtuales** con distribuciones Debian configuradas en VirtualBox
- **Servidor SSH** instalado y configurado en cada VM
- Las VMs deben estar **apagadas** antes de empezar

### Instalar sshpass

```bash
# Ubuntu/Debian
sudo apt-get install sshpass

# macOS
brew install hudochenkov/sshpass/sshpass

# Fedora
sudo dnf install sshpass
```

---

## Estructura del Proyecto

```
vm-platform/
├── main.go                      # Punto de entrada del servidor web
├── go.mod                       # Módulo de Go
├── handlers/
│   └── handlers.go              # Controladores HTTP (API REST)
├── models/
│   └── models.go                # Modelos de datos
├── services/
│   ├── vbox_service.go          # Servicio VBoxManage (operaciones con VMs)
│   ├── ssh_service.go           # Servicio SSH (generación de llaves RSA-1024)
│   └── platform_service.go      # Servicio principal (orquestación)
├── templates/
│   └── index.html               # Dashboard web (HTML/CSS/JS)
└── static/
    ├── css/
    └── js/
```

---

## Instalación y Ejecución

```bash
# 1. Clonar o copiar el proyecto
cd vm-platform

# 2. Compilar
go build -o vm-platform .

# 3. Ejecutar
./vm-platform
```

El servidor se inicia en **http://localhost:8080**

---

## Funcionalidades Implementadas

### Dashboard Principal
- Vista de tres columnas: VMs Base | Discos Multiconexión | VMs de Usuario
- Actualización automática cada 15 segundos
- Indicadores de estado (llaves creadas, discos, usuarios)
- Botones habilitados/deshabilitados según el estado de las operaciones

### Máquinas Virtuales Base
| Acción | Descripción |
|--------|-------------|
| **Agregar VM Base** | Registra una VM existente de VirtualBox seleccionándola del listado |
| **Crear Llaves Root** | Genera par RSA-1024, inicia la VM, despliega llave pública vía sshpass, verifica acceso por llave, apaga la VM |
| **Descargar Llaves Root** | Descarga la llave privada RSA para acceso SSH como root |
| **Crear Disco Multiconexión** | Convierte el disco de la VM base a tipo multiattach |

### Discos Multiconexión
| Acción | Descripción |
|--------|-------------|
| **Crear VM de Usuario** | Crea una nueva VM usando el disco multiconexión compartido |
| **Desconectar Disco** | Desconecta el disco de la VM base |
| **Conectar Disco** | Reconecta el disco a la VM base |
| **Eliminar Disco** | Elimina el disco (solo si no hay VMs de usuario usándolo) |

### Máquinas Virtuales de Usuario
| Acción | Descripción |
|--------|-------------|
| **Crear Usuario** | Crea un usuario en el SO, genera llaves RSA-1024, despliega llave pública |
| **Descargar Llaves** | Descarga la llave privada del usuario para acceso SSH |
| **Eliminar VM** | Desregistra y elimina la VM de usuario |

---

## API REST

| Método | Endpoint | Descripción |
|--------|----------|-------------|
| GET | `/api/dashboard` | Obtener datos del dashboard |
| GET | `/api/vms/available` | Listar VMs disponibles en VirtualBox |
| POST | `/api/base-vm/add` | Agregar VM base |
| POST | `/api/base-vm/create-keys` | Crear llaves SSH de root |
| GET | `/api/base-vm/download-keys?vm_name=X` | Descargar llave privada de root |
| POST | `/api/disk/create` | Crear disco multiconexión |
| POST | `/api/disk/disconnect` | Desconectar disco |
| POST | `/api/disk/connect` | Conectar disco |
| POST | `/api/disk/delete` | Eliminar disco |
| POST | `/api/user-vm/create` | Crear VM de usuario |
| POST | `/api/user-vm/create-user` | Crear usuario en VM |
| GET | `/api/user-vm/download-keys?vm_name=X` | Descargar llaves de usuario |
| POST | `/api/user-vm/delete` | Eliminar VM de usuario |

---

## Flujo de Trabajo Paso a Paso

1. **Agregar VM Base** → Seleccionar una VM existente de VirtualBox
2. **Crear Llaves Root** → Ingresar contraseña de root → Se generan llaves RSA-1024 → Se despliegan automáticamente
3. **Descargar Llaves Root** → Obtener la llave privada para uso posterior
4. **Crear Disco Multiconexión** → Se convierte el disco de la VM base a tipo multiattach
5. **Crear VM de Usuario** → Nueva VM que comparte el disco base
6. **Crear Usuario** → Se crea cuenta + llaves SSH en la VM de usuario
7. **Descargar Llaves de Usuario** → Acceso SSH sin contraseña

---

## Especificaciones Técnicas

- **Algoritmo de llaves**: RSA con 1024 bits (según requisitos)
- **Formato de llaves**: OpenSSH (compatible con `ssh -i`)
- **Tipo de disco**: Multiattach (VirtualBox)
- **Red**: NAT con port forwarding para SSH
- **Persistencia**: Estado almacenado en `~/.vm-platform/platform_data.json`
- **Llaves**: Almacenadas en `~/.vm-platform/keys/<vm-name>/`

---

## Propuestas de Mejora

1. **Seguridad**: Migrar a llaves RSA-4096 o Ed25519 para mayor seguridad
2. **WebSocket**: Usar WebSockets para actualizaciones en tiempo real del estado de las VMs
3. **Snapshots**: Implementar gestión de snapshots de las VMs
4. **Monitoreo**: Agregar métricas de uso de CPU/RAM de las VMs
5. **Autenticación web**: Agregar login con usuario/contraseña para la plataforma web
6. **Logs**: Sistema de logging detallado para auditoría de operaciones
7. **Clustering**: Soporte para múltiples hosts VirtualBox remotos
8. **Templates**: Biblioteca de templates de VMs preconfiguradas

---

## Conocimientos Relevantes Aprendidos

- Gestión programática de VirtualBox mediante `VBoxManage`
- Generación de pares de llaves RSA en Go (sin dependencias externas)
- Formato OpenSSH para llaves públicas (marshaling manual del formato wire)
- Despliegue automatizado de llaves SSH con `sshpass`
- Discos multiattach en VirtualBox y su gestión
- Arquitectura de servicios en Go con separación de responsabilidades
- Desarrollo de APIs REST en Go con la librería estándar `net/http`
- Persistencia de estado en archivos JSON
- Port forwarding NAT para acceso SSH a VMs
