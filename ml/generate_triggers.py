#!/usr/bin/env python3
"""
CLI wrapper for trigger generation via REST API.
Usage: python generate_triggers.py <json_config>
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
            container=config.get("container", ""),
            custom_labels=config.get("customLabels", {}),
            timezone=config.get("timezone", "UTC"),
            days_to_analyze=config.get("daysToAnalyze", 30)
        )
        
        triggers = generator.generate()
        
        output = {
            "success": True,
            "triggers": generator.to_dict()
        }
        
        if config.get("outputFormat") == "smarthpa":
            output["spec"] = generator.to_smarthpa_spec(
                name=config.get("smartHpaName", "my-smarthpa"),
                namespace=config.get("namespace", "default"),
                hpa_name=config.get("hpaName", "my-hpa")
            )
        
        print(json.dumps(output))
        
    except Exception as e:
        print(json.dumps({"success": False, "error": str(e)}))
        sys.exit(1)


if __name__ == "__main__":
    main()

