"""
Trigger Generator for SmartHPA ML Module
Generates optimal SmartHPA trigger suggestions from detected patterns
"""

import json
import yaml
from datetime import datetime
from typing import Dict, List, Optional
from pathlib import Path

try:
    from .config import (
        PrometheusConfig, 
        MetricLabels, 
        AnalysisConfig, 
        TriggerSuggestion
    )
    from .prometheus_client import PrometheusMetricsClient, MetricsData
    from .pattern_analyzer import PatternAnalyzer, TimePattern
except ImportError:
    from config import (
        PrometheusConfig, 
        MetricLabels, 
        AnalysisConfig, 
        TriggerSuggestion
    )
    from prometheus_client import PrometheusMetricsClient, MetricsData
    from pattern_analyzer import PatternAnalyzer, TimePattern


class TriggerGenerator:
    """
    Main class for generating SmartHPA trigger suggestions.
    
    Pipeline:
    1. Fetch metrics from Prometheus (CPU, Memory, Replicas)
    2. Analyze patterns using ML algorithms
    3. Generate optimized trigger configurations
    4. Output in SmartHPA-compatible format
    
    Example usage:
        generator = TriggerGenerator(
            prometheus_url="http://prometheus:9090",
            namespace="production",
            deployment="my-app"
        )
        triggers = generator.generate()
        generator.save_yaml("triggers.yaml")
    """
    
    def __init__(
        self,
        prometheus_url: str = "http://localhost:9090",
        namespace: str = "",
        deployment: str = "",
        container: str = "",
        custom_labels: Dict[str, str] = None,
        timezone: str = "UTC",
        days_to_analyze: int = 30,
        **kwargs
    ):
        """
        Initialize the trigger generator.
        
        Args:
            prometheus_url: Prometheus server URL
            namespace: Kubernetes namespace to filter
            deployment: Deployment name to filter
            container: Container name to filter
            custom_labels: Additional labels for filtering
            timezone: Timezone for generated triggers
            days_to_analyze: Number of days of historical data to analyze
        """
        # Initialize configurations
        self.prometheus_config = PrometheusConfig(
            url=prometheus_url,
            auth_token=kwargs.get("auth_token"),
            timeout=kwargs.get("timeout", 30)
        )
        
        self.labels = MetricLabels(
            namespace=namespace,
            deployment=deployment,
            container=container,
            custom_labels=custom_labels or {}
        )
        
        self.analysis_config = AnalysisConfig(
            days_to_analyze=days_to_analyze,
            cpu_target_utilization=kwargs.get("cpu_target", 0.7),
            memory_target_utilization=kwargs.get("memory_target", 0.8),
            max_triggers=kwargs.get("max_triggers", 10)
        )
        
        self.timezone = timezone
        
        # Initialize components
        self.prometheus_client = PrometheusMetricsClient(self.prometheus_config)
        self.pattern_analyzer = PatternAnalyzer(self.analysis_config)
        
        # Results
        self.metrics: Optional[MetricsData] = None
        self.patterns: List[TimePattern] = []
        self.suggestions: List[TriggerSuggestion] = []
    
    def generate(self) -> List[TriggerSuggestion]:
        """
        Main method to generate trigger suggestions.
        
        Returns:
            List of TriggerSuggestion objects
        """
        print("=" * 60)
        print("SmartHPA Trigger Generator")
        print("=" * 60)
        print(f"Prometheus: {self.prometheus_config.url}")
        print(f"Labels: {self.labels.to_label_selector()}")
        print(f"Analyzing {self.analysis_config.days_to_analyze} days of data")
        print("=" * 60)
        
        # Step 1: Fetch metrics
        self.metrics = self.prometheus_client.fetch_all_metrics(
            self.labels,
            days=self.analysis_config.days_to_analyze
        )
        
        # Step 2: Analyze patterns
        self.patterns = self.pattern_analyzer.analyze(self.metrics)
        
        if not self.patterns:
            print("No patterns detected. Using default configuration.")
            self.suggestions = [self._create_default_trigger()]
            return self.suggestions
        
        # Step 3: Generate trigger suggestions
        self.suggestions = self._patterns_to_triggers(self.patterns)
        
        # Step 4: Optimize and deduplicate
        self.suggestions = self._optimize_triggers(self.suggestions)
        
        print(f"\nGenerated {len(self.suggestions)} trigger suggestions")
        return self.suggestions
    
    def _patterns_to_triggers(
        self, 
        patterns: List[TimePattern]
    ) -> List[TriggerSuggestion]:
        """Convert detected patterns to trigger suggestions"""
        suggestions = []
        
        # Sort patterns by average replicas (high load patterns first)
        sorted_patterns = sorted(
            patterns, 
            key=lambda p: p.avg_replicas, 
            reverse=True
        )
        
        for i, pattern in enumerate(sorted_patterns):
            # Calculate replica requirements
            min_replicas, max_replicas = self._calculate_replicas(pattern)
            
            # Generate trigger name
            days_str = self._days_to_string(pattern.days)
            name = self._generate_trigger_name(pattern, i)
            
            # Calculate priority (higher load = higher priority)
            priority = self.analysis_config.base_priority - (i * self.analysis_config.priority_step)
            
            suggestion = TriggerSuggestion(
                name=name,
                priority=max(10, priority),
                timezone=self.timezone,
                start_time=f"{pattern.start_hour:02d}:{pattern.start_minute:02d}:00",
                end_time=f"{pattern.end_hour:02d}:{pattern.end_minute:02d}:00",
                recurring=days_str,
                min_replicas=min_replicas,
                max_replicas=max_replicas,
                confidence=pattern.confidence
            )
            
            suggestions.append(suggestion)
        
        return suggestions
    
    def _calculate_replicas(self, pattern: TimePattern) -> tuple:
        """Calculate min and max replicas based on pattern data"""
        avg_replicas = pattern.avg_replicas
        peak_replicas = pattern.peak_replicas
        
        buffer = self.analysis_config.replica_buffer_percent
        
        # Min replicas: average minus buffer, at least 1
        min_replicas = max(1, int(avg_replicas * (1 - buffer)))
        
        # Max replicas: peak plus buffer
        max_replicas = max(
            min_replicas + 1,
            int(peak_replicas * (1 + buffer))
        )
        
        return min_replicas, max_replicas
    
    def _days_to_string(self, days: List[int]) -> str:
        """Convert day numbers to SmartHPA recurring format"""
        day_map = self.analysis_config.weekday_map
        return ",".join(day_map.get(d, str(d)) for d in sorted(days))
    
    def _generate_trigger_name(self, pattern: TimePattern, index: int) -> str:
        """Generate a descriptive trigger name"""
        days = pattern.days
        
        # Determine pattern type
        if days == list(range(5)):  # Mon-Fri
            period = "weekday"
        elif days == [5, 6]:  # Sat-Sun
            period = "weekend"
        elif len(days) == 7:
            period = "daily"
        else:
            period = f"custom-{index + 1}"
        
        # Time period
        if pattern.start_hour < 12:
            time_period = "morning"
        elif pattern.start_hour < 17:
            time_period = "afternoon"
        else:
            time_period = "evening"
        
        # Load level
        if pattern.avg_replicas > 5:
            load = "high-load"
        elif pattern.avg_replicas > 2:
            load = "normal-load"
        else:
            load = "low-load"
        
        return f"{period}-{time_period}-{load}"
    
    def _optimize_triggers(
        self, 
        suggestions: List[TriggerSuggestion]
    ) -> List[TriggerSuggestion]:
        """Optimize and deduplicate triggers"""
        if not suggestions:
            return suggestions
        
        # Limit to max triggers
        if len(suggestions) > self.analysis_config.max_triggers:
            # Keep highest confidence triggers
            suggestions = sorted(
                suggestions, 
                key=lambda s: s.confidence, 
                reverse=True
            )[:self.analysis_config.max_triggers]
        
        # Reassign priorities
        for i, suggestion in enumerate(suggestions):
            suggestion.priority = self.analysis_config.base_priority - (i * self.analysis_config.priority_step)
        
        # Make names unique
        names_seen = {}
        for suggestion in suggestions:
            base_name = suggestion.name
            if base_name in names_seen:
                names_seen[base_name] += 1
                suggestion.name = f"{base_name}-{names_seen[base_name]}"
            else:
                names_seen[base_name] = 1
        
        return suggestions
    
    def _create_default_trigger(self) -> TriggerSuggestion:
        """Create a default trigger when no patterns are detected"""
        return TriggerSuggestion(
            name="default-business-hours",
            priority=100,
            timezone=self.timezone,
            start_time="09:00:00",
            end_time="17:00:00",
            recurring="M,TU,W,TH,F",
            min_replicas=2,
            max_replicas=10,
            confidence=0.5
        )
    
    def to_dict(self) -> List[dict]:
        """Convert suggestions to dictionary format"""
        return [s.to_dict() for s in self.suggestions]
    
    def to_json(self, indent: int = 2) -> str:
        """Convert suggestions to JSON string"""
        return json.dumps(self.to_dict(), indent=indent)
    
    def to_yaml(self) -> str:
        """Convert suggestions to YAML string"""
        return yaml.dump(self.to_dict(), default_flow_style=False)
    
    def to_smarthpa_spec(
        self, 
        name: str, 
        namespace: str,
        hpa_name: str
    ) -> dict:
        """Generate a complete SmartHPA specification"""
        return {
            "apiVersion": "autoscaling.sarabala.io/v1alpha1",
            "kind": "SmartHorizontalPodAutoscaler",
            "metadata": {
                "name": name,
                "namespace": namespace
            },
            "spec": {
                "hpaObjectRef": {
                    "name": hpa_name,
                    "namespace": namespace
                },
                "triggers": self.to_dict()
            }
        }
    
    def save_json(self, filepath: str) -> None:
        """Save triggers to JSON file"""
        with open(filepath, "w") as f:
            f.write(self.to_json())
        print(f"Saved triggers to {filepath}")
    
    def save_yaml(self, filepath: str) -> None:
        """Save triggers to YAML file"""
        with open(filepath, "w") as f:
            f.write(self.to_yaml())
        print(f"Saved triggers to {filepath}")
    
    def save_smarthpa_yaml(
        self, 
        filepath: str,
        name: str,
        namespace: str,
        hpa_name: str
    ) -> None:
        """Save complete SmartHPA spec to YAML file"""
        spec = self.to_smarthpa_spec(name, namespace, hpa_name)
        with open(filepath, "w") as f:
            yaml.dump(spec, f, default_flow_style=False)
        print(f"Saved SmartHPA spec to {filepath}")
    
    def print_summary(self) -> None:
        """Print a summary of generated triggers"""
        print("\n" + "=" * 60)
        print("TRIGGER SUGGESTIONS SUMMARY")
        print("=" * 60)
        
        if not self.suggestions:
            print("No triggers generated.")
            return
        
        for i, trigger in enumerate(self.suggestions, 1):
            print(f"\n{i}. {trigger.name}")
            print(f"   Priority: {trigger.priority}")
            print(f"   Schedule: {trigger.recurring} {trigger.start_time} - {trigger.end_time}")
            print(f"   Replicas: min={trigger.min_replicas}, max={trigger.max_replicas}")
            print(f"   Confidence: {trigger.confidence:.1%}")
        
        print("\n" + "=" * 60)


def main():
    """CLI entry point for trigger generation"""
    import argparse
    
    parser = argparse.ArgumentParser(
        description="Generate SmartHPA trigger suggestions from Prometheus metrics"
    )
    parser.add_argument(
        "--prometheus-url", 
        default="http://localhost:9090",
        help="Prometheus server URL"
    )
    parser.add_argument(
        "--namespace", 
        default="",
        help="Kubernetes namespace to filter"
    )
    parser.add_argument(
        "--deployment", 
        default="",
        help="Deployment name to filter"
    )
    parser.add_argument(
        "--days", 
        type=int, 
        default=30,
        help="Days of historical data to analyze"
    )
    parser.add_argument(
        "--timezone", 
        default="UTC",
        help="Timezone for triggers"
    )
    parser.add_argument(
        "--output", 
        default="triggers.yaml",
        help="Output file path"
    )
    parser.add_argument(
        "--format", 
        choices=["yaml", "json", "smarthpa"],
        default="yaml",
        help="Output format"
    )
    parser.add_argument(
        "--hpa-name",
        default="my-hpa",
        help="HPA name for SmartHPA spec (used with --format=smarthpa)"
    )
    parser.add_argument(
        "--smarthpa-name",
        default="my-smarthpa",
        help="SmartHPA name (used with --format=smarthpa)"
    )
    
    args = parser.parse_args()
    
    # Create generator
    generator = TriggerGenerator(
        prometheus_url=args.prometheus_url,
        namespace=args.namespace,
        deployment=args.deployment,
        timezone=args.timezone,
        days_to_analyze=args.days
    )
    
    # Generate triggers
    generator.generate()
    
    # Print summary
    generator.print_summary()
    
    # Save output
    if args.format == "json":
        generator.save_json(args.output)
    elif args.format == "smarthpa":
        generator.save_smarthpa_yaml(
            args.output,
            name=args.smarthpa_name,
            namespace=args.namespace or "default",
            hpa_name=args.hpa_name
        )
    else:
        generator.save_yaml(args.output)


if __name__ == "__main__":
    main()

