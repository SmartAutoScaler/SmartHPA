# SmartHPA (Smart Horizontal Pod Autoscaler)

[![Build and Test](https://github.com/sarabala1979/SmartHPA/actions/workflows/build-test.yaml/badge.svg)](https://github.com/sarabala1979/SmartHPA/actions/workflows/build-test.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sarabala1979/SmartHPA)](https://goreportcard.com/report/github.com/sarabala1979/SmartHPA)
[![codecov](https://codecov.io/gh/sarabala1979/SmartHPA/branch/main/graph/badge.svg)](https://codecov.io/gh/sarabala1979/SmartHPA)

<!-- Uncomment after setting up Gist (see docs/COVERAGE_BADGE_SETUP.md)
[![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/sarabala1979/YOUR_GIST_ID/raw/smarthpa-coverage.json)](https://github.com/sarabala1979/SmartHPA/actions/workflows/build-test.yaml)
-->

SmartHPA is a Kubernetes controller that extends the standard HPA (Horizontal Pod Autoscaler) functionality by adding time-based scaling capabilities. It allows you to define different scaling configurations for different time windows, making it perfect for workloads with predictable traffic patterns. In production systems, SmartHPA will automatically balance your HPA—fully done for you (DFY)—requiring no manual intervention. By intelligently managing your pod scaling, SmartHPA typically reduces resource costs by approximately 30% compared to standard HPA implementations.

## Description

SmartHPA provides intelligent pod scaling based on time windows, recurring schedules, and AI/ML-driven scheduling. Key features include:

- **Time-based Scaling**: Define different HPA configurations for specific time windows
- **Timezone Support**: Schedule scaling operations in your preferred timezone
- **Recurring Schedules**: Set up recurring schedules (e.g., business hours, weekends)
- **Flexible Configuration**: Configure min replicas, max replicas, and desired replicas for each time window
- **Multiple Triggers**: Define multiple triggers with different schedules and configurations
- **Priority-based Overlap Resolution**: Higher priority triggers take precedence during overlapping time windows
- **Web UI**: Manage SmartHPA resources through an intuitive web interface
- **AI-Powered Scheduling** 🚧: ML algorithms automatically generate optimal scaling schedules (in progress)

## Features

| Feature | Status | Description |
|---------|--------|-------------|
| Controller | ✅ Ready | Core SmartHPA controller with time-based scaling |
| Web UI | ✅ Ready | Web interface for CRUD operations on SmartHPA |
| User Auth | ✅ Ready | Login with RBAC permissions (ConfigMap-based) |
| ML Service | 🚧 In Progress | Auto-generate triggers from Prometheus metrics |
| AI Scheduler | 🚧 Planned | Fully automated DFY scaling |

## Container Image

The SmartHPA container image is available on Quay.io:

```sh
quay.io/sarabala1979/smarthpa:latest
```

You can also use specific version tags like `v1.0.0` or `v1.0`.

## Quick Start

### Prerequisites
- Kubernetes cluster v1.11.3+
- kubectl version v1.11.3+

### 1. Install the Controller

**Option A: One-line installation**
```sh
kubectl apply -f https://raw.githubusercontent.com/sarabala1979/SmartHPA/main/install.yaml
```

**Option B: Using local file**
```sh
kubectl apply -f install.yaml
```

This installs:
- SmartHPA Custom Resource Definition (CRD)
- RBAC configurations (ServiceAccount, ClusterRole, ClusterRoleBinding)
- SmartHPA controller deployment

**Verify installation:**
```sh
kubectl get pods -n smarthpa-system
kubectl get crd smarthorizontalpodautoscalers.autoscaling.sarabala.io
```

### 2. Create Your First SmartHPA

```sh
# Apply the example HPA and SmartHPA
kubectl apply -f config/samples/example-hpa.yaml
```

### 3. Deploy the UI (Optional)

The SmartHPA UI provides a web interface for managing SmartHPA resources.

**Option A: Deploy to Kubernetes cluster**
```sh
# Apply UI deployment and service
kubectl apply -k config/ui/

# Verify deployment
kubectl get pods -n smarthpa-system -l app=smarthpa-ui
kubectl get svc -n smarthpa-system smarthpa-ui
```

**Access the UI in cluster:**
```sh
# Port forward to access locally
kubectl port-forward -n smarthpa-system svc/smarthpa-ui 8090:80

# Or use NodePort (port 30090)
# http://<node-ip>:30090

# Or configure Ingress (edit config/ui/ingress.yaml first)
kubectl apply -f config/ui/ingress.yaml
```

**Option B: Run locally with the binary**
```sh
# Start UI server on port 8090
./bin/manager --server --server-addr=:8090
```

**Option C: Run from source**
```sh
go run ./cmd/main.go --server --server-addr=:8090
```

**Access the UI:**
- Open http://localhost:8090 in your browser
- Login page: http://localhost:8090/login.html

**Default credentials** (configured via ConfigMap):
| User | Password | Permission |
|------|----------|------------|
| admin | admin123 | Full access (7) |
| editor | editor123 | Read+Write (6) |
| viewer | viewer123 | Read only (4) |

**Configure users:**
```sh
kubectl apply -f config/samples/smarthpa-users-configmap.yaml
```

**UI Deployment Architecture:**
```
┌──────────────────────────────────────────────────────────────┐
│                    smarthpa-system namespace                  │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌─────────────────┐     ┌─────────────────┐                │
│  │ smarthpa-ui     │     │ smarthpa-       │                │
│  │ Deployment      │     │ controller      │                │
│  │ (REST + WebUI)  │     │ Deployment      │                │
│  └────────┬────────┘     └─────────────────┘                │
│           │                                                  │
│  ┌────────┴────────┐                                        │
│  │ smarthpa-ui     │◄──── ClusterIP:80                      │
│  │ Service         │◄──── NodePort:30090                    │
│  └────────┬────────┘                                        │
│           │                                                  │
│  ┌────────┴────────┐                                        │
│  │ Ingress         │◄──── smarthpa.example.com              │
│  │ (optional)      │                                        │
│  └─────────────────┘                                        │
└──────────────────────────────────────────────────────────────┘
```

### 4. Run Both Controller and UI

```sh
# Run controller + UI together
go run ./cmd/main.go --controller --server --server-addr=:8090
```

### 5. ML Service (🚧 In Progress)

> **Note:** The ML-powered trigger generation service is currently under development.

The ML module will automatically analyze Prometheus metrics and generate optimal SmartHPA trigger configurations based on:
- Peak hours detection (high traffic periods)
- Off-peak periods (low traffic)
- Weekend patterns
- Seasonal variations

**Preview (development mode):**
```sh
# Start ML service on port 8091
go run ./cmd/main.go --ml --ml-addr=:8091 --ml-prometheus-url=http://prometheus:9090

# Test trigger generation
curl -X POST http://localhost:8091/api/v1/ml/generate \
  -H "Content-Type: application/json" \
  -d '{"namespace":"production","deployment":"my-app","daysToAnalyze":30}'
```

---

## Development Setup

If you want to build from source or contribute to SmartHPA, you'll need:
- go version v1.22.0+
- docker version 17.03+
- Python 3.11+ (for ML module)

**Build and push your image:**
```sh
make docker-build docker-push IMG=<registry>/smarthpa:<tag>
```

**Deploy to cluster:**
```sh
make deploy IMG=<registry>/smarthpa:<tag>
```

**Run locally:**
```sh
make run
```

**Try out the examples:**
```sh
kubectl apply -k config/samples/
```

---

## Uninstallation

**Using install.yaml:**
```sh
kubectl delete -f install.yaml
```

**For development setup:**
```sh
make undeploy
make uninstall
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/smarthpa:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/smarthpa/<tag or branch>/dist/install.yaml
```

## High-Level Approach

SmartHPA works by:

1. **Monitoring Time Windows**: Continuously monitors the current time against defined time windows
2. **Applying Configurations**: Automatically applies the appropriate HPA configuration based on the current time window
3. **Scheduling Changes**: Sets up cron jobs to handle transitions between different time windows
4. **Managing HPA**: Updates the underlying HPA object with new configurations when time windows change
5. **AI-Driven Scheduling**: Uses machine learning algorithms to analyze application metrics and create optimal scaling schedules

## Architecture & Design

- **Controller + Analysis module design doc**: see `docs/ANALYSIS_MODULE_DESIGN.md`

## AI-Powered Scheduling (🚧 In Progress)

> **Status:** The ML-powered scheduling system is currently under active development.

SmartHPA will include a fully automated, Done For You (DFY) AI/ML-based scheduling system that manages your application scaling without manual intervention:

- **Zero Configuration Required**: Simply enable AI and let SmartHPA handle everything
- **Intelligent Metric Analysis**: AI analyzes 30 days of Prometheus metrics (CPU, memory, replica counts)
- **Pattern Detection**: Automatically detects peak hours, off-peak periods, weekend patterns, and seasonal variations
- **Smart Schedule Generation**: Creates optimized scaling schedules based on real-world usage
- **Cost Optimization**: Balances performance and resource costs, targeting up to 30% reduction in resource costs

**Current ML Pipeline (v2):**
```
Prometheus (30 days) → Data Fetcher → Preprocessor → ML Model → Trigger Generator
```

**Detected Pattern Types:**
| Pattern | Description |
|---------|-------------|
| Peak | High traffic periods (e.g., 9 AM - 5 PM weekdays) |
| Off-Peak | Low traffic periods (e.g., nights) |
| Weekend | Saturday/Sunday patterns |
| Seasonal | Recurring time-based patterns |

For traditional manual scheduling, configure triggers directly in your SmartHPA spec.

## Example

Here's an example SmartHPA configuration that scales differently during business hours and weekends:

```yaml
apiVersion: autoscaling.sarabala.io/v1alpha1
kind: SmartHorizontalPodAutoscaler
metadata:
  name: example-smarthpa
  namespace: default
spec:
  HPAObjectRef:
    namespace: default
    name: example-hpa
  triggers:
    - name: business-hours
      priority: 10
      interval:
        timezone: "America/Los_Angeles"
        recurring: "M,TU,W,TH,F"
      startHPAConfig:
        minReplicas: 3
        maxReplicas: 10
        desiredReplicas: 5
      endHPAConfig:
        minReplicas: 1
        maxReplicas: 3
        desiredReplicas: 1
      startTime: "09:00:00"
      endTime: "18:00:00"
    - name: weekend-scale-down
      priority: 5
      interval:
        timezone: "America/Los_Angeles"
        recurring: "SAT,SUN"
      startTime: "18:00:00"
      endTime: "23:59:59"
      startHPAConfig:
        minReplicas: 1
        maxReplicas: 2
        desiredReplicas: 1
```

This configuration:
- Scales up during business hours (9 AM to 6 PM Pacific Time) on weekdays
- Maintains lower scaling limits during weekends
- Automatically transitions between configurations based on the schedule

### Priority Feature

The `priority` field allows you to control which schedule takes effect when multiple schedules overlap. A schedule with a higher `priority` value will always take precedence over lower-priority schedules during overlapping periods. This makes it easy to configure short-term or one-off scaling events, such as for marketing campaigns or Black Friday sales, without disrupting your regular scaling logic.

For example, if you add a trigger for a special event with a higher priority than your normal business-hours trigger, the special event's configuration will be applied during the overlap. Once the event ends, the system will automatically revert to the next highest-priority active schedule.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request. For major changes, please open an issue first to discuss what you would like to change.

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

