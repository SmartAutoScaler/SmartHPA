"""
Configuration for SmartHPA ML Module v2
"""

from dataclasses import dataclass, field
from typing import Dict, List, Optional
from enum import Enum


class TriggerType(Enum):
    """Types of triggers the model can detect"""
    PEAK = "peak"
    OFF_PEAK = "off-peak"
    WEEKEND = "weekend"
    WEEKDAY = "weekday"
    SEASONAL = "seasonal"
    NIGHT = "night"
    MORNING = "morning"
    AFTERNOON = "afternoon"
    EVENING = "evening"


@dataclass
class PrometheusConfig:
    """Prometheus connection settings"""
    url: str = "http://localhost:9090"
    timeout: int = 30
    
    # Query templates - {labels} will be replaced
    replica_query: str = "kube_deployment_status_replicas{{{labels}}}"
    hpa_replica_query: str = "kube_horizontalpodautoscaler_status_current_replicas{{{labels}}}"


@dataclass  
class FetchConfig:
    """Data fetching configuration"""
    days: int = 30
    step_minutes: int = 5  # 5-minute intervals
    namespace: str = ""
    deployment: str = ""
    custom_labels: Dict[str, str] = field(default_factory=dict)
    
    def to_label_selector(self) -> str:
        """Build Prometheus label selector"""
        labels = []
        if self.namespace:
            labels.append(f'namespace="{self.namespace}"')
        if self.deployment:
            labels.append(f'deployment="{self.deployment}"')
        for k, v in self.custom_labels.items():
            labels.append(f'{k}="{v}"')
        return ",".join(labels)


@dataclass
class ModelConfig:
    """Model configuration"""
    # Pattern detection thresholds
    peak_percentile: float = 85.0  # Top 15% is peak
    off_peak_percentile: float = 30.0  # Bottom 30% is off-peak
    
    # Time windows
    business_hours_start: int = 9
    business_hours_end: int = 17
    night_hours_start: int = 22
    night_hours_end: int = 6
    
    # Weekend days (0=Monday, 6=Sunday)
    weekend_days: List[int] = field(default_factory=lambda: [5, 6])
    
    # Minimum confidence for trigger
    min_confidence: float = 0.6
    
    # Replica buffer for safety
    replica_buffer_percent: float = 0.2


@dataclass
class TriggerConfig:
    """Generated trigger configuration"""
    name: str
    trigger_type: TriggerType
    priority: int
    timezone: str
    start_time: str  # HH:MM:SS
    end_time: str
    recurring: str  # M,TU,W,TH,F,SAT,SUN
    min_replicas: int
    max_replicas: int
    confidence: float
    
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
                "maxReplicas": max(2, self.min_replicas)
            }
        }


@dataclass
class PatternResult:
    """Detected pattern from model"""
    pattern_type: TriggerType
    hours: List[int]  # Hours when this pattern is active
    days: List[int]  # Days of week (0=Mon, 6=Sun)
    avg_replicas: float
    min_replicas: float
    max_replicas: float
    percentile_25: float
    percentile_75: float
    confidence: float
    sample_count: int

