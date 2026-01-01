"""
Configuration for SmartHPA ML Module
"""

from dataclasses import dataclass, field
from typing import Dict, List, Optional


@dataclass
class PrometheusConfig:
    """Prometheus connection configuration"""
    url: str = "http://localhost:9090"
    timeout: int = 30
    verify_ssl: bool = True
    auth_token: Optional[str] = None


@dataclass
class MetricLabels:
    """Configurable labels for Prometheus queries"""
    namespace: str = ""
    deployment: str = ""
    pod: str = ""
    container: str = ""
    custom_labels: Dict[str, str] = field(default_factory=dict)
    
    def to_label_selector(self) -> str:
        """Convert to Prometheus label selector string"""
        labels = []
        if self.namespace:
            labels.append(f'namespace="{self.namespace}"')
        if self.deployment:
            labels.append(f'deployment="{self.deployment}"')
        if self.pod:
            labels.append(f'pod=~"{self.pod}.*"')
        if self.container:
            labels.append(f'container="{self.container}"')
        for key, value in self.custom_labels.items():
            labels.append(f'{key}="{value}"')
        return ",".join(labels)


@dataclass
class AnalysisConfig:
    """Configuration for pattern analysis"""
    # Time range
    days_to_analyze: int = 30
    step_minutes: int = 5  # Data resolution
    
    # Pattern detection
    min_pattern_duration_hours: float = 1.0
    max_triggers: int = 10
    
    # Clustering
    n_clusters_range: tuple = (2, 8)  # Min/max clusters to try
    
    # Replica calculation
    cpu_target_utilization: float = 0.7  # 70% target CPU
    memory_target_utilization: float = 0.8  # 80% target memory
    replica_buffer_percent: float = 0.2  # 20% buffer for min/max
    
    # Priority assignment
    base_priority: int = 100
    priority_step: int = 10
    
    # Days of week mapping
    weekday_map: Dict[int, str] = field(default_factory=lambda: {
        0: "M", 1: "TU", 2: "W", 3: "TH", 4: "F", 5: "SAT", 6: "SUN"
    })


@dataclass
class TriggerSuggestion:
    """Represents a suggested trigger"""
    name: str
    priority: int
    timezone: str
    start_time: str
    end_time: str
    recurring: str  # e.g., "M,TU,W,TH,F"
    min_replicas: int
    max_replicas: int
    confidence: float  # 0-1 confidence score
    
    def to_dict(self) -> dict:
        """Convert to SmartHPA trigger format"""
        return {
            "name": self.name,
            "priority": self.priority,
            "timezone": self.timezone,
            "startTime": self.start_time,
            "endTime": self.end_time,
            "interval": {"recurring": self.recurring},
            "startHPAConfig": {
                "minReplicas": self.min_replicas,
                "maxReplicas": self.max_replicas
            },
            "endHPAConfig": {
                "minReplicas": 1,
                "maxReplicas": max(3, self.min_replicas)
            }
        }

