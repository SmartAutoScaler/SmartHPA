# SmartHPA ML Module v2
# Generic workflow for replica pattern analysis and trigger generation

from .pipeline import TriggerPipeline
from .data_fetcher import PrometheusDataFetcher
from .preprocessor import DataPreprocessor
from .model import ReplicaPatternModel
from .postprocessor import TriggerPostProcessor

__version__ = "2.0.0"
__all__ = [
    "TriggerPipeline",
    "PrometheusDataFetcher", 
    "DataPreprocessor",
    "ReplicaPatternModel",
    "TriggerPostProcessor"
]



