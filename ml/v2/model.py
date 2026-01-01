"""
Replica Pattern Model - Analyzes replica patterns and detects peak/off-peak/weekend/seasonal triggers
"""

import numpy as np
from typing import List, Tuple
from dataclasses import dataclass

from .config import ModelConfig, PatternResult, TriggerType
from .preprocessor import ProcessedData


class ReplicaPatternModel:
    """
    Generic model for replica pattern analysis.
    
    Detects patterns:
    - PEAK: High traffic periods (>85th percentile)
    - OFF_PEAK: Low traffic periods (<30th percentile)  
    - WEEKEND: Saturday/Sunday patterns
    - WEEKDAY: Monday-Friday patterns
    - SEASONAL: Time-of-day patterns (morning, afternoon, evening, night)
    
    Algorithm:
    1. Identify peak/off-peak thresholds from percentiles
    2. Analyze weekday vs weekend patterns
    3. Detect time-of-day patterns
    4. Calculate confidence based on consistency
    """
    
    def __init__(self, config: ModelConfig = None):
        self.config = config or ModelConfig()
    
    def infer(self, data: ProcessedData) -> List[PatternResult]:
        """
        Run inference on preprocessed data.
        
        Args:
            data: Preprocessed replica metrics
            
        Returns:
            List of detected patterns
        """
        if data.global_max == 0:
            return []
        
        patterns = []
        
        # 1. Detect weekday business hours pattern (PEAK)
        weekday_peak = self._detect_weekday_peak(data)
        if weekday_peak:
            patterns.append(weekday_peak)
        
        # 2. Detect weekday off-peak (5 PM - 9 AM)
        weekday_offpeak = self._detect_weekday_offpeak(data)
        if weekday_offpeak:
            patterns.append(weekday_offpeak)
        
        # 3. Detect weekday night (late night)
        weekday_night = self._detect_weekday_night(data)
        if weekday_night:
            patterns.append(weekday_night)
        
        # 4. Detect weekend pattern
        weekend = self._detect_weekend(data)
        if weekend:
            patterns.append(weekend)
        
        # 5. Detect morning ramp-up
        morning = self._detect_morning(data)
        if morning:
            patterns.append(morning)
        
        # 6. Detect evening wind-down
        evening = self._detect_evening(data)
        if evening:
            patterns.append(evening)
        
        # Filter by minimum confidence
        patterns = [p for p in patterns if p.confidence >= self.config.min_confidence]
        
        print(f"✓ Model detected {len(patterns)} patterns")
        for p in patterns:
            print(f"  - {p.pattern_type.value}: avg={p.avg_replicas:.1f}, "
                  f"max={p.max_replicas:.0f}, conf={p.confidence:.1%}")
        
        return patterns
    
    def _detect_weekday_offpeak(self, data: ProcessedData) -> PatternResult:
        """Detect weekday off-peak pattern (5 PM - 9 AM)"""
        weekday_indices = list(range(5))
        
        # Off-peak hours: 5 PM (17) to 9 AM (8:59)
        offpeak_hours = list(range(17, 24)) + list(range(0, 9))
        
        values = []
        max_values = []
        for d in weekday_indices:
            for h in offpeak_hours:
                if data.hourly_count[d, h] > 0:
                    values.append(data.hourly_avg[d, h])
                    max_values.append(data.hourly_max[d, h])
        
        if not values:
            return None
        
        values = np.array(values)
        max_values = np.array(max_values)
        avg = np.mean(values)
        
        # Check if this is actually off-peak (below global median)
        if avg > data.percentile_50:
            return None
        
        if len(values) > 1:
            cv = np.std(values) / (np.mean(values) + 1e-10)
            confidence = max(0.5, 1.0 - cv)
        else:
            confidence = 0.6
        
        return PatternResult(
            pattern_type=TriggerType.OFF_PEAK,
            hours=offpeak_hours,
            days=weekday_indices,
            avg_replicas=float(avg),
            min_replicas=float(np.min(values)),
            max_replicas=float(np.max(max_values)),
            percentile_25=float(np.percentile(values, 25)),
            percentile_75=float(np.percentile(values, 75)),
            confidence=confidence,
            sample_count=len(values)
        )
    
    def _detect_weekday_peak(self, data: ProcessedData) -> PatternResult:
        """Detect weekday peak hours pattern"""
        # Business hours on weekdays
        weekday_indices = list(range(5))  # Mon-Fri
        peak_hours = list(range(
            self.config.business_hours_start, 
            self.config.business_hours_end
        ))
        
        # Get all values for weekday peak hours (avg and max)
        avg_values = []
        max_values = []
        for d in weekday_indices:
            for h in peak_hours:
                if data.hourly_count[d, h] > 0:
                    avg_values.append(data.hourly_avg[d, h])
                    max_values.append(data.hourly_max[d, h])
        
        if not avg_values:
            return None
        
        avg_values = np.array(avg_values)
        max_values = np.array(max_values)
        avg = np.mean(avg_values)
        
        # Check if this is actually a peak (higher than off-peak)
        offpeak_avg = np.mean(data.daily_avg) * 0.8
        if avg < offpeak_avg:
            return None
        
        # Calculate confidence based on consistency
        if len(avg_values) > 1:
            cv = np.std(avg_values) / (np.mean(avg_values) + 1e-10)
            confidence = max(0.5, 1.0 - cv)
        else:
            confidence = 0.6
        
        return PatternResult(
            pattern_type=TriggerType.PEAK,
            hours=peak_hours,
            days=weekday_indices,
            avg_replicas=float(avg),
            min_replicas=float(np.min(avg_values)),
            max_replicas=float(np.max(max_values)),  # Use max of max for burst capacity
            percentile_25=float(np.percentile(avg_values, 25)),
            percentile_75=float(np.percentile(avg_values, 75)),
            confidence=confidence,
            sample_count=len(avg_values)
        )
    
    def _detect_weekday_night(self, data: ProcessedData) -> PatternResult:
        """Detect weekday night/off-peak pattern"""
        weekday_indices = list(range(5))
        
        # Night hours (10 PM - 6 AM)
        night_hours = list(range(self.config.night_hours_start, 24)) + \
                     list(range(0, self.config.night_hours_end))
        
        values = []
        for d in weekday_indices:
            for h in night_hours:
                if data.hourly_count[d, h] > 0:
                    values.append(data.hourly_avg[d, h])
        
        if not values:
            return None
        
        values = np.array(values)
        avg = np.mean(values)
        
        # Check if this is actually off-peak (below global 50th percentile)
        if avg > data.percentile_50:
            return None
        
        if len(values) > 1:
            cv = np.std(values) / (np.mean(values) + 1e-10)
            confidence = max(0.5, 1.0 - cv)
        else:
            confidence = 0.6
        
        return PatternResult(
            pattern_type=TriggerType.NIGHT,
            hours=night_hours,
            days=weekday_indices,
            avg_replicas=float(avg),
            min_replicas=float(np.min(values)),
            max_replicas=float(np.max(values)),
            percentile_25=float(np.percentile(values, 25)),
            percentile_75=float(np.percentile(values, 75)),
            confidence=confidence,
            sample_count=len(values)
        )
    
    def _detect_weekend(self, data: ProcessedData) -> PatternResult:
        """Detect weekend pattern"""
        weekend_indices = self.config.weekend_days
        all_hours = list(range(24))
        
        values = []
        for d in weekend_indices:
            for h in all_hours:
                if data.hourly_count[d, h] > 0:
                    values.append(data.hourly_avg[d, h])
        
        if not values:
            return None
        
        values = np.array(values)
        avg = np.mean(values)
        
        # Compare to weekday average
        weekday_avg = np.mean(data.daily_avg[:5])
        
        # Weekend should be different from weekday
        if abs(avg - weekday_avg) < data.global_std * 0.5:
            return None
        
        if len(values) > 1:
            cv = np.std(values) / (np.mean(values) + 1e-10)
            confidence = max(0.5, 1.0 - cv)
        else:
            confidence = 0.6
        
        return PatternResult(
            pattern_type=TriggerType.WEEKEND,
            hours=all_hours,
            days=weekend_indices,
            avg_replicas=float(avg),
            min_replicas=float(np.min(values)),
            max_replicas=float(np.max(values)),
            percentile_25=float(np.percentile(values, 25)),
            percentile_75=float(np.percentile(values, 75)),
            confidence=confidence,
            sample_count=len(values)
        )
    
    def _detect_morning(self, data: ProcessedData) -> PatternResult:
        """Detect morning ramp-up pattern"""
        weekday_indices = list(range(5))
        morning_hours = list(range(6, self.config.business_hours_start))
        
        values = []
        for d in weekday_indices:
            for h in morning_hours:
                if data.hourly_count[d, h] > 0:
                    values.append(data.hourly_avg[d, h])
        
        if not values or len(values) < 5:
            return None
        
        values = np.array(values)
        avg = np.mean(values)
        
        # Morning should be between night and peak
        if avg <= data.percentile_25 or avg >= data.percentile_75:
            return None
        
        if len(values) > 1:
            cv = np.std(values) / (np.mean(values) + 1e-10)
            confidence = max(0.4, 1.0 - cv)
        else:
            confidence = 0.5
        
        return PatternResult(
            pattern_type=TriggerType.MORNING,
            hours=morning_hours,
            days=weekday_indices,
            avg_replicas=float(avg),
            min_replicas=float(np.min(values)),
            max_replicas=float(np.max(values)),
            percentile_25=float(np.percentile(values, 25)),
            percentile_75=float(np.percentile(values, 75)),
            confidence=confidence,
            sample_count=len(values)
        )
    
    def _detect_evening(self, data: ProcessedData) -> PatternResult:
        """Detect evening wind-down pattern"""
        weekday_indices = list(range(5))
        evening_hours = list(range(self.config.business_hours_end, self.config.night_hours_start))
        
        values = []
        for d in weekday_indices:
            for h in evening_hours:
                if data.hourly_count[d, h] > 0:
                    values.append(data.hourly_avg[d, h])
        
        if not values or len(values) < 5:
            return None
        
        values = np.array(values)
        avg = np.mean(values)
        
        # Evening should be between night and peak
        if avg <= data.percentile_25 or avg >= data.percentile_75:
            return None
        
        if len(values) > 1:
            cv = np.std(values) / (np.mean(values) + 1e-10)
            confidence = max(0.4, 1.0 - cv)
        else:
            confidence = 0.5
        
        return PatternResult(
            pattern_type=TriggerType.EVENING,
            hours=evening_hours,
            days=weekday_indices,
            avg_replicas=float(avg),
            min_replicas=float(np.min(values)),
            max_replicas=float(np.max(values)),
            percentile_25=float(np.percentile(values, 25)),
            percentile_75=float(np.percentile(values, 75)),
            confidence=confidence,
            sample_count=len(values)
        )

