# SmartHPA (Smart Horizontal Pod Autoscaler)

[![Build and Test](https://github.com/sarabala1979/SmartHPA/actions/workflows/build-test.yaml/badge.svg)](https://github.com/sarabala1979/SmartHPA/actions/workflows/build-test.yaml)

SmartHPA is a Kubernetes controller that extends the standard HPA (Horizontal Pod Autoscaler) functionality by adding time-based scaling capabilities. It allows you to define different scaling configurations for different time windows, making it perfect for workloads with predictable traffic patterns. In production systems, SmartHPA will automatically balance your HPA—fully done for you (DFY)—requiring no manual intervention. By intelligently managing your pod scaling, SmartHPA typically reduces resource costs by approximately 30% compared to standard HPA implementations.

## Description

SmartHPA provides intelligent pod scaling based on time windows, recurring schedules, and AI/ML-driven scheduling. Key features include:

- **Time-based Scaling**: Define different HPA configurations for specific time windows
- **Timezone Support**: Schedule scaling operations in your preferred timezone
- **Recurring Schedules**: Set up recurring schedules (e.g., business hours, weekends)
- **Flexible Configuration**: Configure min replicas, max replicas, and desired replicas for each time window
- **Multiple Triggers**: Define multiple triggers with different schedules and configurations
- **AI-Powered Scheduling**: AI/ML algorithms automatically generate optimal scaling schedules based on application metrics and traffic patterns

## Container Image

The SmartHPA container image is available on Quay.io:

```sh
quay.io/sarabala1979/smarthpa:latest
```

You can also use specific version tags like `v1.0.0` or `v1.0`.

## Getting Started

### Prerequisites
- Kubernetes cluster v1.11.3+
- kubectl version v1.11.3+

### Quick Installation

To quickly install SmartHPA in your cluster:

```sh
kubectl apply -f install.yaml
```

This will install:
- SmartHPA Custom Resource Definition (CRD)
- RBAC configurations (ServiceAccount, ClusterRole, ClusterRoleBinding)
- SmartHPA controller deployment

### Development Setup

If you want to build from source or contribute to SmartHPA, you'll need:
- go version v1.22.0+
- docker version 17.03+

**Build and push your image:**

```sh
make docker-build docker-push IMG=<registry>/smarthpa:<tag>
```

**Deploy to cluster:**

```sh
make deploy IMG=<registry>/smarthpa:<tag>
```

**Try out the examples:**

```sh
kubectl apply -k config/samples/
```

### Uninstallation

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

## AI-Powered Scheduling (Done For You)

SmartHPA includes a fully automated, Done For You (DFY) AI/ML-based scheduling system that completely manages your application scaling without manual intervention:

- **Zero Configuration Required**: Simply enable AI and let SmartHPA handle everything
- **Intelligent Metric Analysis**: AI continuously analyzes application metrics, resource usage, and traffic patterns
- **Smart Schedule Generation**: Automatically creates and optimizes scaling schedules based on real-world usage
- **Self-Learning System**: Continuously adapts and improves schedules as your application's usage patterns evolve
- **Cost Optimization**: Automatically balances performance and resource costs, achieving up to 30% reduction in resource costs
- **Predictive Scaling**: Anticipates traffic spikes and adjusts scaling preemptively
- **Manual Override Available**: Retain control with the ability to override AI decisions when needed

To experience fully automated scaling, simply set `enableAI: true` in your SmartHPA configuration. The AI system will handle everything else, from analysis to implementation. For traditional manual scheduling, set `enableAI: false`.

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

