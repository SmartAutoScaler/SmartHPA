#!/usr/bin/env python3
"""
CLI wrapper for pattern analysis via REST API.
Usage: python analyze_patterns.py <json_config>
"""

import sys
import os
import json

# Ensure we're running from the correct directory
script_dir = os.path.dirname(os.path.abspath(__file__))
os.chdir(script_dir)
sys.path.insert(0, script_dir)

from trigger_generator import TriggerGenerator


def main():
    if len(sys.argv) < 2:
        print(json.dumps({"success": False, "error": "No configuration provided"}))
        sys.exit(1)
    
    try:
        config = json.loads(sys.argv[1])
    except json.JSONDecodeError as e:
        print(json.dumps({"success": False, "error": f"Invalid JSON: {e}"}))
        sys.exit(1)
    
    try:
        generator = TriggerGenerator(
            prometheus_url=config.get("prometheusUrl", "http://localhost:9090"),
            namespace=config.get("namespace", ""),
            deployment=config.get("deployment", ""),
            custom_labels=config.get("customLabels", {}),
            days_to_analyze=config.get("daysToAnalyze", 30)
        )
        
        # Fetch and analyze metrics
        generator.metrics = generator.prometheus_client.fetch_all_metrics(
            generator.labels,
            days=generator.analysis_config.days_to_analyze
        )
        generator.patterns = generator.pattern_analyzer.analyze(generator.metrics)
        
        # Convert patterns to serializable format
        patterns = []
        for p in generator.patterns:
            patterns.append({
                "startHour": p.start_hour,
                "startMinute": p.start_minute,
                "endHour": p.end_hour,
                "endMinute": p.end_minute,
                "days": p.days,
                "avgCpu": p.avg_cpu,
                "avgMemory": p.avg_memory,
                "avgReplicas": p.avg_replicas,
                "peakReplicas": p.peak_replicas,
                "confidence": p.confidence
            })
        
        # Stats
        stats = {
            "cpuDataPoints": len(generator.metrics.cpu) if not generator.metrics.cpu.empty else 0,
            "memoryDataPoints": len(generator.metrics.memory) if not generator.metrics.memory.empty else 0,
            "replicaDataPoints": len(generator.metrics.replicas) if not generator.metrics.replicas.empty else 0,
            "patternsDetected": len(patterns)
        }
        
        output = {
            "success": True,
            "patterns": patterns,
            "stats": stats
        }
        
        print(json.dumps(output))
        
    except Exception as e:
        print(json.dumps({"success": False, "error": str(e)}))
        sys.exit(1)


if __name__ == "__main__":
    main()

