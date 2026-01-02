#!/usr/bin/env python3
"""
Example usage of the SmartHPA ML Trigger Generator

This script demonstrates how to:
1. Connect to Prometheus and fetch metrics
2. Analyze patterns in the data
3. Generate optimal trigger suggestions
4. Save them in SmartHPA-compatible format
"""

import sys
sys.path.insert(0, '..')

from trigger_generator import TriggerGenerator
from config import MetricLabels


def example_basic_usage():
    """Basic usage with minimal configuration"""
    print("\n" + "=" * 60)
    print("EXAMPLE 1: Basic Usage")
    print("=" * 60)
    
    generator = TriggerGenerator(
        prometheus_url="http://localhost:9090",
        namespace="production",
        deployment="my-app"
    )
    
    triggers = generator.generate()
    generator.print_summary()
    
    # Save as YAML
    generator.save_yaml("basic_triggers.yaml")


def example_custom_labels():
    """Usage with custom Prometheus labels"""
    print("\n" + "=" * 60)
    print("EXAMPLE 2: Custom Labels")
    print("=" * 60)
    
    generator = TriggerGenerator(
        prometheus_url="http://prometheus.monitoring:9090",
        namespace="ecommerce",
        deployment="checkout-service",
        container="app",
        custom_labels={
            "env": "production",
            "region": "us-west-2",
            "team": "platform"
        },
        timezone="America/Los_Angeles",
        days_to_analyze=14
    )
    
    triggers = generator.generate()
    generator.print_summary()


def example_full_smarthpa_spec():
    """Generate complete SmartHPA specification"""
    print("\n" + "=" * 60)
    print("EXAMPLE 3: Full SmartHPA Spec")
    print("=" * 60)
    
    generator = TriggerGenerator(
        prometheus_url="http://localhost:9090",
        namespace="default",
        deployment="web-frontend",
        timezone="UTC",
        days_to_analyze=30,
        cpu_target=0.65,  # 65% CPU target
        memory_target=0.75,  # 75% memory target
        max_triggers=5
    )
    
    triggers = generator.generate()
    generator.print_summary()
    
    # Save complete SmartHPA spec
    generator.save_smarthpa_yaml(
        filepath="web-frontend-smarthpa.yaml",
        name="web-frontend-smarthpa",
        namespace="default",
        hpa_name="web-frontend-hpa"
    )
    
    # Also print the spec
    import yaml
    spec = generator.to_smarthpa_spec(
        name="web-frontend-smarthpa",
        namespace="default",
        hpa_name="web-frontend-hpa"
    )
    print("\nGenerated SmartHPA Spec:")
    print(yaml.dump(spec, default_flow_style=False))


def example_programmatic_access():
    """Access triggers programmatically"""
    print("\n" + "=" * 60)
    print("EXAMPLE 4: Programmatic Access")
    print("=" * 60)
    
    generator = TriggerGenerator(
        prometheus_url="http://localhost:9090",
        namespace="api",
        deployment="gateway"
    )
    
    triggers = generator.generate()
    
    # Access individual triggers
    for trigger in triggers:
        print(f"\nTrigger: {trigger.name}")
        print(f"  Priority: {trigger.priority}")
        print(f"  Start: {trigger.start_time}")
        print(f"  End: {trigger.end_time}")
        print(f"  Days: {trigger.recurring}")
        print(f"  Min Replicas: {trigger.min_replicas}")
        print(f"  Max Replicas: {trigger.max_replicas}")
        print(f"  Confidence: {trigger.confidence:.1%}")
        
        # Get as dict
        trigger_dict = trigger.to_dict()
        print(f"  As dict: {trigger_dict}")


def example_with_sample_data():
    """Run with sample data (no Prometheus required)"""
    print("\n" + "=" * 60)
    print("EXAMPLE 5: Sample Data (No Prometheus)")
    print("=" * 60)
    
    # When Prometheus is unavailable, the module generates
    # realistic sample data for demonstration
    generator = TriggerGenerator(
        prometheus_url="http://unavailable:9090",  # Will use sample data
        namespace="demo",
        deployment="sample-app",
        timezone="America/New_York",
        days_to_analyze=30
    )
    
    triggers = generator.generate()
    generator.print_summary()
    
    print("\nJSON Output:")
    print(generator.to_json())


if __name__ == "__main__":
    # Run all examples
    example_with_sample_data()
    
    # Uncomment to run other examples (requires Prometheus):
    # example_basic_usage()
    # example_custom_labels()
    # example_full_smarthpa_spec()
    # example_programmatic_access()



