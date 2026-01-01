#!/usr/bin/env python3
"""
API Handler for ML service
Receives JSON config, runs pipeline, returns JSON response
"""

import sys
import os
import json

# Ensure correct path
script_dir = os.path.dirname(os.path.abspath(__file__))
parent_dir = os.path.dirname(script_dir)
sys.path.insert(0, parent_dir)
os.chdir(script_dir)

from v2.pipeline import TriggerPipeline


def generate_triggers(config: dict) -> dict:
    """
    Generate triggers from config.
    
    Args:
        config: {
            "prometheusUrl": str,
            "namespace": str,
            "deployment": str,
            "customLabels": dict,
            "timezone": str,
            "daysToAnalyze": int,
            "outputFormat": str,  # yaml, json, smarthpa
            "smartHpaName": str,
            "hpaName": str
        }
    
    Returns:
        {
            "success": bool,
            "triggers": list,
            "spec": dict (if smarthpa format),
            "stats": dict,
            "error": str (if failed)
        }
    """
    try:
        # Create pipeline
        pipeline = TriggerPipeline(
            prometheus_url=config.get("prometheusUrl", "http://localhost:9090"),
            namespace=config.get("namespace", ""),
            deployment=config.get("deployment", ""),
            custom_labels=config.get("customLabels", {}),
            timezone=config.get("timezone", "UTC"),
            days=config.get("daysToAnalyze", 30),
            step_minutes=config.get("stepMinutes", 5)
        )
        
        # Run pipeline
        triggers = pipeline.run()
        
        # Build response
        response = {
            "success": True,
            "triggers": pipeline.to_dict(),
            "stats": pipeline.get_stats()
        }
        
        # Add SmartHPA spec if requested
        if config.get("outputFormat") == "smarthpa":
            response["spec"] = pipeline.to_smarthpa_spec(
                name=config.get("smartHpaName", "my-smarthpa"),
                namespace=config.get("namespace", "default"),
                hpa_name=config.get("hpaName", "my-hpa")
            )
        
        return response
        
    except Exception as e:
        return {
            "success": False,
            "error": str(e)
        }


def main():
    """CLI entry point - reads JSON config from argv"""
    if len(sys.argv) < 2:
        print(json.dumps({"success": False, "error": "No config provided"}))
        sys.exit(1)
    
    try:
        config = json.loads(sys.argv[1])
    except json.JSONDecodeError as e:
        print(json.dumps({"success": False, "error": f"Invalid JSON: {e}"}))
        sys.exit(1)
    
    # Suppress print output for API mode
    import io
    from contextlib import redirect_stdout
    
    # Capture stdout
    stdout_capture = io.StringIO()
    
    with redirect_stdout(stdout_capture):
        result = generate_triggers(config)
    
    # Output only JSON
    print(json.dumps(result))


if __name__ == "__main__":
    main()

