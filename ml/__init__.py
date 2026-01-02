# SmartHPA ML Module
# Generates optimal trigger suggestions based on Prometheus metrics

from .trigger_generator import TriggerGenerator
from .prometheus_client import PrometheusMetricsClient
from .pattern_analyzer import PatternAnalyzer

__version__ = "1.0.0"
__all__ = ["TriggerGenerator", "PrometheusMetricsClient", "PatternAnalyzer"]



