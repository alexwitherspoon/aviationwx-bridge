# Dynamic Resource Limits

AviationWX Bridge automatically adjusts its resource consumption based on the host system's available resources. This ensures optimal performance on everything from a Pi Zero 2 W (416MB RAM) to a Pi 4 8GB or dedicated server.

## Overview

The bridge **never takes priority over the operating system**, ensuring the host remains responsive for:
- SSH access
- Watchdog scripts
- Supervisor updates
- System recovery operations

## How It Works

### 1. Resource Detection

On container startup, `aviationwx-container-start.sh` detects:
- **Total system memory** (from `/proc/meminfo`)
- **Total CPU cores** (from `nproc`)

### 2. Dynamic Calculation

Based on detected resources, the script calculates appropriate limits:

#### Memory Allocation Strategy

| System RAM | Docker Limit | Go Heap Limit | tmpfs Size | Example Device |
|------------|--------------|---------------|------------|----------------|
| < 1GB      | 60% of RAM   | 85% of Docker | 100MB      | Pi Zero 2 W    |
| 1-2GB      | 65% of RAM   | 88% of Docker | 150MB      | Pi 3B+         |
| 2-4GB      | 70% of RAM   | 90% of Docker | 200MB      | Pi 4 2GB       |
| 4-8GB      | 75% of RAM   | 90% of Docker | 300MB      | Pi 4 4GB       |
| > 8GB      | 80% of RAM   | 90% of Docker | 500MB      | Pi 4 8GB       |

**Example (Pi Zero 2 W with 416MB):**
```
Total RAM:          416 MB
Docker Limit:       249 MB  (60%)
  Go Heap:          211 MB  (85% of Docker)
  Stack/Runtime:     20 MB
  Native (ffmpeg):   18 MB
OS Reserved:        167 MB  (40%)
tmpfs Queue:        100 MB
```

#### CPU Allocation Strategy

| System Cores | Docker Limit | CPU Shares | Reserved for OS |
|--------------|--------------|------------|-----------------|
| 1 core       | 1.0          | 1024 (100%) | 0 (no choice)  |
| 2 cores      | 1.5          | 768 (75%)   | 0.5 cores      |
| 3 cores      | 2.0          | 768 (75%)   | 1 core         |
| 4+ cores     | N-1          | 768 (75%)   | 1 core         |

**CPU Shares Explained:**
- Bridge: 768 (75% priority)
- OS processes (SSH, watchdog): 1024 (100% priority)
- OS always wins in CPU contention

### 3. Docker Resource Limits Applied

The container is started with:

```bash
docker run -d \
    --memory="249m" \              # Hard limit (OOM kills container, not host)
    --memory-reservation="211m" \  # Soft limit (reclaim under pressure)
    --memory-swap="249m" \         # Total = memory (no additional swap)
    --oom-kill-disable=false \     # Let Docker handle OOM (protects host)
    --cpus="3" \                   # Max CPU usage
    --cpu-shares=768 \             # Lower priority than OS (1024)
    --pids-limit=200 \             # Prevent fork bombs
    -e "GOMEMLIMIT=211MiB" \       # Go runtime heap limit
    --tmpfs "/dev/shm:size=100m" \ # Queue storage
    ...
```

## Benefits

### 🛡️ Host Protection
- **Hard memory limits** prevent OOM killer from targeting host processes
- **CPU shares** ensure OS always gets priority
- **Swap = Memory** prevents SD card thrashing
- **PID limits** prevent fork bombs

### 🚀 Performance Scaling
- **Pi Zero 2 W:** Conservative (249MB) - stable, no OOM
- **Pi 4 4GB:** Generous (3GB) - fast, can handle many cameras
- **Server 16GB:** Maximum (12GB+) - enterprise-grade

### 🔧 Zero Configuration
- Automatically adapts to hardware
- No manual tuning required
- Same Docker image works everywhere
- Installer handles everything

## Resource Limits in Action

### Pi Zero 2 W (416MB RAM, 4 cores)
```
System resources detected:
  Total Memory: 416 MB
  Total CPUs: 4

Calculated resource limits:
  Docker Memory Limit: 249 MB
  Go Memory Limit (GOMEMLIMIT): 211 MB
  tmpfs Size: 100 MB
  Docker CPU Limit: 3 CPUs
  CPU Shares: 768
```

**Result:**
- Bridge uses max 249MB (OS has 167MB free)
- Bridge uses max 3 cores (OS has 1 core reserved)
- SD card safe (swap disabled)
- System always responsive

### Pi 4 4GB (4096MB RAM, 4 cores)
```
Calculated resource limits:
  Docker Memory Limit: 3072 MB
  Go Memory Limit (GOMEMLIMIT): 2764 MB
  tmpfs Size: 300 MB
  Docker CPU Limit: 3 CPUs
  CPU Shares: 768
```

**Result:**
- 12x more memory than Pi Zero (can handle 20+ cameras)
- Same CPU allocation (3 cores, 1 for OS)
- Faster image processing
- Still protects host

## Emergency Scenarios

### Scenario 1: Container Hits Memory Limit
**What happens:**
1. Go GC triggers at 211MB (85% of limit)
2. If that fails, Docker kills container at 249MB
3. systemd restarts container (10s delay)
4. Watchdog detects unhealthy state
5. Normal operation resumes

**Host impact:** None (OS still has 167MB free)

### Scenario 2: CPU-Intensive Operation
**What happens:**
1. Bridge tries to use all CPU
2. Docker limits to 3 cores
3. CPU shares give OS priority (1024 vs 768)
4. SSH/watchdog remain responsive

**Host impact:** Minimal (1 core always reserved)

### Scenario 3: Memory Leak in Bridge
**What happens:**
1. Go heap grows toward limit
2. GC becomes more aggressive
3. Eventually hits Docker hard limit
4. Container killed, not host
5. systemd restarts with clean state

**Host impact:** None (leak contained)

## Manual Override (Advanced)

If you need to override the automatic limits, edit `/etc/systemd/system/aviationwx-container.service`:

```ini
[Service]
Environment="DOCKER_MEMORY_OVERRIDE=512m"
Environment="DOCKER_CPUS_OVERRIDE=2"
Environment="GOMEMLIMIT_OVERRIDE=450MiB"
```

Then restart:
```bash
sudo systemctl daemon-reload
sudo systemctl restart aviationwx-container
```

**Warning:** Overriding limits may cause OOM kills on the host. Only do this if you understand the risks.

## Monitoring

### Check Current Limits
```bash
# View container limits
docker inspect aviationwx-bridge | jq '.[0].HostConfig | {Memory, MemoryReservation, MemorySwap, NanoCpus, CpuShares}'

# View actual usage
docker stats aviationwx-bridge --no-stream
```

### Check Go Memory Limit
```bash
# From inside container
docker exec aviationwx-bridge sh -c 'echo $GOMEMLIMIT'

# From logs
docker logs aviationwx-bridge 2>&1 | grep "Resource limits"
```

## Trade-offs

| Limit Type | Benefit | Trade-off | Mitigation |
|------------|---------|-----------|------------|
| Memory < 300MB | Protects host | Container may be killed | Queue thinning, resource limiter |
| CPU < N cores | OS responsive | Slower processing | Acceptable for reliability |
| CPU shares 75% | OS priority | None | Pure upside |
| No additional swap | SD card safe | Earlier OOM | Better than swap death |

## Design Philosophy

**Fail forward, protect the host:**
1. Host OS survival > Bridge uptime
2. Bridge can restart, host cannot
3. Container death is recoverable, host OOM is not
4. Better to restart cleanly than thrash to death

## References

- [Docker Runtime Options](https://docs.docker.com/config/containers/resource_constraints/)
- [Go Memory Limit (GOMEMLIMIT)](https://tip.golang.org/doc/gc-guide#Memory_limit)
- [cgroup v1/v2 Memory Controller](https://www.kernel.org/doc/Documentation/cgroup-v1/memory.txt)
