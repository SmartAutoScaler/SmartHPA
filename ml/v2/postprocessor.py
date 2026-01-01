"""
Post Processor - Convert model patterns to SmartHPA trigger configurations
"""

import numpy as np
from typing import List, Dict

from .config import (
    ModelConfig, 
    PatternResult, 
    TriggerConfig, 
    TriggerType
)
from .preprocessor import ProcessedData


class TriggerPostProcessor:
    """
    Converts detected patterns to SmartHPA trigger configurations.
    
    Workflow:
    1. Sort patterns by priority (peak > weekday > morning/evening > weekend > night)
    2. Calculate min/max replicas with safety buffer
    3. Generate time windows
    4. Assign priorities
    5. Return trigger configurations
    """
    
    # Pattern priority order
    PRIORITY_ORDER = {
        TriggerType.PEAK: 100,
        TriggerType.WEEKDAY: 90,
        TriggerType.MORNING: 80,
        TriggerType.EVENING: 75,
        TriggerType.AFTERNOON: 70,
        TriggerType.WEEKEND: 60,
        TriggerType.NIGHT: 50,
        TriggerType.OFF_PEAK: 40,
        TriggerType.SEASONAL: 30,
    }
    
    # Day name mapping
    DAY_NAMES = {
        0: "M",
        1: "TU", 
        2: "W",
        3: "TH",
        4: "F",
        5: "SAT",
        6: "SUN"
    }
    
    def __init__(self, config: ModelConfig = None, timezone: str = "UTC"):
        self.config = config or ModelConfig()
        self.timezone = timezone
    
    def process(
        self, 
        patterns: List[PatternResult],
        data: ProcessedData
    ) -> List[TriggerConfig]:
        """
        Convert patterns to trigger configurations.
        
        Args:
            patterns: Detected patterns from model
            data: Preprocessed data for replica calculations
            
        Returns:
            List of trigger configurations
        """
        if not patterns:
            return self._default_triggers(data)
        
        triggers = []
        
        for pattern in patterns:
            trigger = self._pattern_to_trigger(pattern, data)
            if trigger:
                triggers.append(trigger)
        
        # Sort by priority (highest first)
        triggers.sort(key=lambda t: t.priority, reverse=True)
        
        # Deduplicate overlapping triggers
        triggers = self._deduplicate(triggers)
        
        print(f"✓ Generated {len(triggers)} trigger configurations")
        
        return triggers
    
    def _pattern_to_trigger(
        self, 
        pattern: PatternResult,
        data: ProcessedData
    ) -> TriggerConfig:
        """Convert a single pattern to trigger config"""
        
        # Calculate replica counts with buffer
        buffer = self.config.replica_buffer_percent
        
        # Use percentile_25 for min (with buffer)
        min_replicas = max(1, int(pattern.min_replicas))
        
        # Use max_replicas with buffer for max (to handle bursts)
        max_replicas = max(
            min_replicas + 2,
            int(pattern.max_replicas * (1 + buffer))
        )
        
        # For peak patterns, ensure we capture burst capacity
        if pattern.pattern_type == TriggerType.PEAK:
            # If there's high variance, increase max
            if pattern.percentile_75 > pattern.avg_replicas * 1.2:
                max_replicas = max(max_replicas, int(pattern.max_replicas * 1.5))
        
        # Get time window
        start_hour = min(pattern.hours)
        end_hour = max(pattern.hours) + 1
        if end_hour > 23:
            end_hour = 23
        
        start_time = f"{start_hour:02d}:00:00"
        end_time = f"{end_hour:02d}:00:00"
        
        # Get recurring days
        recurring = ",".join(self.DAY_NAMES[d] for d in sorted(pattern.days))
        
        # Get priority
        priority = self.PRIORITY_ORDER.get(pattern.pattern_type, 50)
        
        # Generate name
        name = self._generate_name(pattern)
        
        return TriggerConfig(
            name=name,
            trigger_type=pattern.pattern_type,
            priority=priority,
            timezone=self.timezone,
            start_time=start_time,
            end_time=end_time,
            recurring=recurring,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
            confidence=pattern.confidence
        )
    
    def _generate_name(self, pattern: PatternResult) -> str:
        """Generate descriptive trigger name"""
        type_name = pattern.pattern_type.value
        
        # Add day context
        if pattern.days == [5, 6]:
            day_ctx = "weekend"
        elif pattern.days == list(range(5)):
            day_ctx = "weekday"
        else:
            day_ctx = "custom"
        
        # Add time context
        if pattern.hours[0] >= 22 or pattern.hours[0] < 6:
            time_ctx = "night"
        elif pattern.hours[0] < 9:
            time_ctx = "morning"
        elif pattern.hours[0] < 12:
            time_ctx = "late-morning"
        elif pattern.hours[0] < 17:
            time_ctx = "afternoon"
        else:
            time_ctx = "evening"
        
        return f"{day_ctx}-{time_ctx}-{type_name}"
    
    def _deduplicate(self, triggers: List[TriggerConfig]) -> List[TriggerConfig]:
        """Remove overlapping triggers, keeping higher priority ones"""
        if len(triggers) <= 1:
            return triggers
        
        result = []
        seen_windows = set()
        
        for trigger in triggers:
            window_key = (trigger.recurring, trigger.start_time, trigger.end_time)
            if window_key not in seen_windows:
                result.append(trigger)
                seen_windows.add(window_key)
        
        return result
    
    def _default_triggers(self, data: ProcessedData) -> List[TriggerConfig]:
        """Generate default triggers when no patterns detected"""
        if data.global_max == 0:
            # No data - return minimal config
            return [
                TriggerConfig(
                    name="default-baseline",
                    trigger_type=TriggerType.WEEKDAY,
                    priority=100,
                    timezone=self.timezone,
                    start_time="09:00:00",
                    end_time="17:00:00",
                    recurring="M,TU,W,TH,F",
                    min_replicas=2,
                    max_replicas=10,
                    confidence=0.5
                )
            ]
        
        # Use global stats for defaults
        buffer = self.config.replica_buffer_percent
        min_rep = max(1, int(data.percentile_25 * (1 - buffer)))
        max_rep = max(min_rep + 1, int(data.global_max * (1 + buffer)))
        
        return [
            TriggerConfig(
                name="default-business-hours",
                trigger_type=TriggerType.PEAK,
                priority=100,
                timezone=self.timezone,
                start_time="09:00:00",
                end_time="17:00:00",
                recurring="M,TU,W,TH,F",
                min_replicas=min_rep,
                max_replicas=max_rep,
                confidence=0.5
            ),
            TriggerConfig(
                name="default-weekend",
                trigger_type=TriggerType.WEEKEND,
                priority=60,
                timezone=self.timezone,
                start_time="00:00:00",
                end_time="23:59:00",
                recurring="SAT,SUN",
                min_replicas=max(1, min_rep // 2),
                max_replicas=max(2, max_rep // 2),
                confidence=0.5
            )
        ]
    
    def to_smarthpa_spec(
        self,
        triggers: List[TriggerConfig],
        name: str,
        namespace: str,
        hpa_name: str
    ) -> Dict:
        """Generate complete SmartHPA specification"""
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
                "triggers": [t.to_dict() for t in triggers]
            }
        }

