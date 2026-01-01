"""
Trigger Generation Pipeline - Orchestrates the complete ML workflow
"""

import json
import yaml
from typing import List, Dict, Optional
from dataclasses import asdict

from .config import (
    PrometheusConfig,
    FetchConfig,
    ModelConfig,
    TriggerConfig
)
from .data_fetcher import PrometheusDataFetcher, ReplicaMetrics
from .preprocessor import DataPreprocessor, ProcessedData
from .model import ReplicaPatternModel, PatternResult
from .postprocessor import TriggerPostProcessor


class TriggerPipeline:
    """
    Complete ML pipeline for generating SmartHPA triggers.
    
    Workflow:
    ┌─────────────────┐
    │   Prometheus    │
    │   (30 days)     │
    └────────┬────────┘
             │
             ▼
    ┌─────────────────┐
    │  Data Fetcher   │  → Fetch replicas every 5 mins
    └────────┬────────┘
             │
             ▼
    ┌─────────────────┐
    │  Preprocessor   │  → Clean, aggregate, statistics
    └────────┬────────┘
             │
             ▼
    ┌─────────────────┐
    │     Model       │  → Detect peak/off-peak/weekend/seasonal
    └────────┬────────┘
             │
             ▼
    ┌─────────────────┐
    │ Post Processor  │  → Convert to trigger configs
    └────────┬────────┘
             │
             ▼
    ┌─────────────────┐
    │   Triggers      │  → SmartHPA-compatible output
    └─────────────────┘
    
    Usage:
        pipeline = TriggerPipeline(
            prometheus_url="http://prometheus:9090",
            namespace="production",
            deployment="my-app"
        )
        triggers = pipeline.run()
        print(pipeline.to_yaml())
    """
    
    def __init__(
        self,
        prometheus_url: str = "http://localhost:9090",
        namespace: str = "",
        deployment: str = "",
        custom_labels: Dict[str, str] = None,
        timezone: str = "UTC",
        days: int = 30,
        step_minutes: int = 5
    ):
        """
        Initialize the pipeline.
        
        Args:
            prometheus_url: Prometheus server URL
            namespace: Kubernetes namespace
            deployment: Deployment name
            custom_labels: Additional label filters
            timezone: Timezone for triggers
            days: Days of history to analyze
            step_minutes: Data point interval
        """
        # Configuration
        self.prom_config = PrometheusConfig(url=prometheus_url)
        self.fetch_config = FetchConfig(
            days=days,
            step_minutes=step_minutes,
            namespace=namespace,
            deployment=deployment,
            custom_labels=custom_labels or {}
        )
        self.model_config = ModelConfig()
        self.timezone = timezone
        
        # Pipeline components
        self.fetcher = PrometheusDataFetcher(self.prom_config)
        self.preprocessor = DataPreprocessor()
        self.model = ReplicaPatternModel(self.model_config)
        self.postprocessor = TriggerPostProcessor(self.model_config, timezone)
        
        # Results
        self.metrics: Optional[ReplicaMetrics] = None
        self.processed: Optional[ProcessedData] = None
        self.patterns: List[PatternResult] = []
        self.triggers: List[TriggerConfig] = []
    
    def run(self) -> List[TriggerConfig]:
        """
        Execute the complete pipeline.
        
        Returns:
            List of generated trigger configurations
        """
        print("=" * 60)
        print("SmartHPA Trigger Pipeline v2")
        print("=" * 60)
        print(f"Prometheus: {self.prom_config.url}")
        print(f"Labels: {self.fetch_config.to_label_selector()}")
        print(f"Period: {self.fetch_config.days} days @ {self.fetch_config.step_minutes}min")
        print("=" * 60)
        
        # Step 1: Fetch data
        print("\n[1/4] Fetching replica metrics...")
        self.metrics = self.fetcher.fetch(self.fetch_config)
        
        if self.metrics.is_empty:
            print("⚠ No data available")
            return []
        
        # Step 2: Preprocess
        print("\n[2/4] Preprocessing data...")
        self.processed = self.preprocessor.process(self.metrics)
        
        # Step 3: Model inference
        print("\n[3/4] Running model inference...")
        self.patterns = self.model.infer(self.processed)
        
        # Step 4: Post-process to triggers
        print("\n[4/4] Generating trigger configurations...")
        self.triggers = self.postprocessor.process(self.patterns, self.processed)
        
        print("\n" + "=" * 60)
        print("Pipeline Complete")
        print("=" * 60)
        
        return self.triggers
    
    def to_dict(self) -> List[Dict]:
        """Convert triggers to dictionary format"""
        return [t.to_dict() for t in self.triggers]
    
    def to_json(self, indent: int = 2) -> str:
        """Convert triggers to JSON string"""
        return json.dumps(self.to_dict(), indent=indent)
    
    def to_yaml(self) -> str:
        """Convert triggers to YAML string"""
        return yaml.dump(self.to_dict(), default_flow_style=False)
    
    def to_smarthpa_spec(
        self,
        name: str,
        namespace: str = None,
        hpa_name: str = None
    ) -> Dict:
        """Generate complete SmartHPA specification"""
        ns = namespace or self.fetch_config.namespace or "default"
        hpa = hpa_name or f"{self.fetch_config.deployment}-hpa"
        
        return self.postprocessor.to_smarthpa_spec(
            self.triggers, name, ns, hpa
        )
    
    def to_smarthpa_yaml(
        self,
        name: str,
        namespace: str = None,
        hpa_name: str = None
    ) -> str:
        """Generate SmartHPA YAML"""
        spec = self.to_smarthpa_spec(name, namespace, hpa_name)
        return yaml.dump(spec, default_flow_style=False)
    
    def print_summary(self) -> None:
        """Print a summary of generated triggers"""
        print("\n" + "=" * 60)
        print("TRIGGER SUMMARY")
        print("=" * 60)
        
        if not self.triggers:
            print("No triggers generated.")
            return
        
        for i, t in enumerate(self.triggers, 1):
            print(f"\n{i}. {t.name}")
            print(f"   Type: {t.trigger_type.value}")
            print(f"   Priority: {t.priority}")
            print(f"   Schedule: {t.recurring} {t.start_time}-{t.end_time}")
            print(f"   Replicas: min={t.min_replicas}, max={t.max_replicas}")
            print(f"   Confidence: {t.confidence:.1%}")
        
        print("\n" + "=" * 60)
    
    def get_stats(self) -> Dict:
        """Get pipeline statistics"""
        stats = {
            "data_points": self.metrics.count if self.metrics else 0,
            "patterns_detected": len(self.patterns),
            "triggers_generated": len(self.triggers),
        }
        
        if self.processed:
            stats.update({
                "global_avg_replicas": round(self.processed.global_avg, 2),
                "global_max_replicas": int(self.processed.global_max),
                "global_min_replicas": int(self.processed.global_min),
            })
        
        return stats


def main():
    """CLI entry point"""
    import argparse
    
    parser = argparse.ArgumentParser(
        description="Generate SmartHPA triggers from Prometheus metrics"
    )
    parser.add_argument("--prometheus-url", default="http://localhost:9090")
    parser.add_argument("--namespace", default="")
    parser.add_argument("--deployment", default="")
    parser.add_argument("--days", type=int, default=30)
    parser.add_argument("--timezone", default="UTC")
    parser.add_argument("--output", default="triggers.yaml")
    parser.add_argument("--format", choices=["yaml", "json", "smarthpa"], default="yaml")
    parser.add_argument("--name", default="my-smarthpa")
    parser.add_argument("--hpa-name", default="")
    
    args = parser.parse_args()
    
    pipeline = TriggerPipeline(
        prometheus_url=args.prometheus_url,
        namespace=args.namespace,
        deployment=args.deployment,
        timezone=args.timezone,
        days=args.days
    )
    
    pipeline.run()
    pipeline.print_summary()
    
    # Output
    if args.format == "json":
        output = pipeline.to_json()
    elif args.format == "smarthpa":
        output = pipeline.to_smarthpa_yaml(
            name=args.name,
            hpa_name=args.hpa_name or None
        )
    else:
        output = pipeline.to_yaml()
    
    with open(args.output, "w") as f:
        f.write(output)
    
    print(f"\nSaved to {args.output}")


if __name__ == "__main__":
    main()

