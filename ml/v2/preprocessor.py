"""
Data Preprocessor - Clean and transform replica metrics for model input
"""

import numpy as np
from dataclasses import dataclass
from typing import Dict, List, Tuple

from .data_fetcher import ReplicaMetrics


@dataclass
class ProcessedData:
    """Preprocessed data ready for model inference"""
    # Original metrics
    raw_metrics: ReplicaMetrics
    
    # Hourly aggregations (24 hours x 7 days matrix)
    hourly_avg: np.ndarray  # Shape: (7, 24) - avg replicas by day/hour
    hourly_max: np.ndarray
    hourly_min: np.ndarray
    hourly_std: np.ndarray
    hourly_count: np.ndarray  # Sample count per cell
    
    # Daily aggregations
    daily_avg: np.ndarray  # Shape: (7,) - avg by day of week
    daily_max: np.ndarray
    daily_min: np.ndarray
    
    # Overall statistics
    global_avg: float
    global_max: float
    global_min: float
    global_std: float
    percentile_25: float
    percentile_50: float
    percentile_75: float
    percentile_90: float
    percentile_95: float


class DataPreprocessor:
    """
    Preprocesses replica metrics for model inference.
    
    Workflow:
    1. Clean data (remove outliers, fill gaps)
    2. Aggregate by hour and day of week
    3. Calculate statistics
    4. Return structured ProcessedData
    """
    
    def __init__(self, outlier_std: float = 3.0):
        """
        Args:
            outlier_std: Remove values beyond N standard deviations
        """
        self.outlier_std = outlier_std
    
    def process(self, metrics: ReplicaMetrics) -> ProcessedData:
        """
        Process raw metrics into model-ready format.
        
        Args:
            metrics: Raw replica metrics from Prometheus
            
        Returns:
            ProcessedData with aggregations and statistics
        """
        if metrics.is_empty:
            return self._empty_processed_data(metrics)
        
        # Step 1: Clean data
        values = self._clean_data(metrics.values)
        
        # Step 2: Calculate global statistics
        global_stats = self._calculate_global_stats(values)
        
        # Step 3: Build hourly aggregation matrix (7 days x 24 hours)
        hourly_agg = self._build_hourly_matrix(
            values, 
            metrics.hours, 
            metrics.days_of_week
        )
        
        # Step 4: Build daily aggregations
        daily_agg = self._build_daily_aggregations(
            values,
            metrics.days_of_week
        )
        
        print(f"✓ Preprocessed {len(values)} data points")
        print(f"  Global avg: {global_stats['avg']:.1f}, "
              f"min: {global_stats['min']:.0f}, "
              f"max: {global_stats['max']:.0f}")
        
        return ProcessedData(
            raw_metrics=metrics,
            hourly_avg=hourly_agg['avg'],
            hourly_max=hourly_agg['max'],
            hourly_min=hourly_agg['min'],
            hourly_std=hourly_agg['std'],
            hourly_count=hourly_agg['count'],
            daily_avg=daily_agg['avg'],
            daily_max=daily_agg['max'],
            daily_min=daily_agg['min'],
            global_avg=global_stats['avg'],
            global_max=global_stats['max'],
            global_min=global_stats['min'],
            global_std=global_stats['std'],
            percentile_25=global_stats['p25'],
            percentile_50=global_stats['p50'],
            percentile_75=global_stats['p75'],
            percentile_90=global_stats['p90'],
            percentile_95=global_stats['p95'],
        )
    
    def _clean_data(self, values: np.ndarray) -> np.ndarray:
        """Remove outliers and clean data"""
        if len(values) == 0:
            return values
        
        # Remove extreme outliers (beyond N std)
        mean = np.mean(values)
        std = np.std(values)
        
        if std > 0:
            lower = mean - self.outlier_std * std
            upper = mean + self.outlier_std * std
            mask = (values >= lower) & (values <= upper)
            cleaned = values[mask]
        else:
            cleaned = values
        
        # Ensure minimum of 1 replica
        cleaned = np.maximum(cleaned, 1)
        
        return cleaned
    
    def _calculate_global_stats(self, values: np.ndarray) -> Dict:
        """Calculate global statistics"""
        if len(values) == 0:
            return {k: 0.0 for k in ['avg', 'max', 'min', 'std', 'p25', 'p50', 'p75', 'p90', 'p95']}
        
        return {
            'avg': float(np.mean(values)),
            'max': float(np.max(values)),
            'min': float(np.min(values)),
            'std': float(np.std(values)),
            'p25': float(np.percentile(values, 25)),
            'p50': float(np.percentile(values, 50)),
            'p75': float(np.percentile(values, 75)),
            'p90': float(np.percentile(values, 90)),
            'p95': float(np.percentile(values, 95)),
        }
    
    def _build_hourly_matrix(
        self, 
        values: np.ndarray,
        hours: np.ndarray,
        days: np.ndarray
    ) -> Dict[str, np.ndarray]:
        """Build 7x24 matrix of hourly aggregations"""
        # Initialize matrices
        avg_matrix = np.zeros((7, 24))
        max_matrix = np.zeros((7, 24))
        min_matrix = np.full((7, 24), np.inf)
        std_matrix = np.zeros((7, 24))
        count_matrix = np.zeros((7, 24))
        
        # Collect values by day/hour
        buckets = [[[] for _ in range(24)] for _ in range(7)]
        
        for i, val in enumerate(values):
            if i < len(hours) and i < len(days):
                d = int(days[i])
                h = int(hours[i])
                buckets[d][h].append(val)
        
        # Calculate aggregations
        for d in range(7):
            for h in range(24):
                bucket = buckets[d][h]
                if bucket:
                    avg_matrix[d, h] = np.mean(bucket)
                    max_matrix[d, h] = np.max(bucket)
                    min_matrix[d, h] = np.min(bucket)
                    std_matrix[d, h] = np.std(bucket)
                    count_matrix[d, h] = len(bucket)
                else:
                    min_matrix[d, h] = 0
        
        return {
            'avg': avg_matrix,
            'max': max_matrix,
            'min': min_matrix,
            'std': std_matrix,
            'count': count_matrix
        }
    
    def _build_daily_aggregations(
        self,
        values: np.ndarray,
        days: np.ndarray
    ) -> Dict[str, np.ndarray]:
        """Build daily aggregations"""
        daily_avg = np.zeros(7)
        daily_max = np.zeros(7)
        daily_min = np.full(7, np.inf)
        
        buckets = [[] for _ in range(7)]
        for i, val in enumerate(values):
            if i < len(days):
                buckets[int(days[i])].append(val)
        
        for d in range(7):
            if buckets[d]:
                daily_avg[d] = np.mean(buckets[d])
                daily_max[d] = np.max(buckets[d])
                daily_min[d] = np.min(buckets[d])
            else:
                daily_min[d] = 0
        
        return {
            'avg': daily_avg,
            'max': daily_max,
            'min': daily_min
        }
    
    def _empty_processed_data(self, metrics: ReplicaMetrics) -> ProcessedData:
        """Return empty processed data"""
        return ProcessedData(
            raw_metrics=metrics,
            hourly_avg=np.zeros((7, 24)),
            hourly_max=np.zeros((7, 24)),
            hourly_min=np.zeros((7, 24)),
            hourly_std=np.zeros((7, 24)),
            hourly_count=np.zeros((7, 24)),
            daily_avg=np.zeros(7),
            daily_max=np.zeros(7),
            daily_min=np.zeros(7),
            global_avg=0.0,
            global_max=0.0,
            global_min=0.0,
            global_std=0.0,
            percentile_25=0.0,
            percentile_50=0.0,
            percentile_75=0.0,
            percentile_90=0.0,
            percentile_95=0.0,
        )

