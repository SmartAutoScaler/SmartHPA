"""
Pattern Analyzer for SmartHPA ML Module
Uses ML algorithms to detect usage patterns and suggest optimal triggers
"""

import pandas as pd
import numpy as np
from datetime import datetime, timedelta
from typing import Dict, List, Optional, Tuple
from dataclasses import dataclass
from collections import defaultdict

from sklearn.cluster import KMeans, DBSCAN
from sklearn.preprocessing import StandardScaler
from sklearn.metrics import silhouette_score

try:
    from .config import AnalysisConfig, TriggerSuggestion
    from .prometheus_client import MetricsData
except ImportError:
    from config import AnalysisConfig, TriggerSuggestion
    from prometheus_client import MetricsData


@dataclass
class TimePattern:
    """Detected time-based pattern"""
    start_hour: int
    start_minute: int
    end_hour: int
    end_minute: int
    days: List[int]  # 0=Monday, 6=Sunday
    avg_cpu: float
    avg_memory: float
    avg_replicas: float
    peak_replicas: int
    confidence: float


class PatternAnalyzer:
    """
    Analyzes metrics data to detect recurring patterns using ML algorithms.
    
    Algorithms used:
    - K-Means clustering for grouping similar time periods
    - DBSCAN for outlier detection
    - Time series decomposition for trend analysis
    """
    
    def __init__(self, config: AnalysisConfig = None):
        self.config = config or AnalysisConfig()
        self.scaler = StandardScaler()
    
    def analyze(self, metrics: MetricsData) -> List[TimePattern]:
        """
        Main analysis pipeline.
        Returns detected time patterns from the metrics data.
        """
        print("Analyzing metrics patterns...")
        
        # Step 1: Prepare unified time series
        df = self._prepare_time_series(metrics)
        if df.empty:
            print("Warning: No data to analyze")
            return []
        
        # Step 2: Extract time features
        df = self._extract_time_features(df)
        
        # Step 3: Detect patterns using clustering
        patterns = self._detect_patterns_with_clustering(df)
        
        # Step 4: Refine patterns with statistical analysis
        refined_patterns = self._refine_patterns(patterns, df)
        
        # Step 5: Merge similar patterns
        merged_patterns = self._merge_similar_patterns(refined_patterns)
        
        print(f"Detected {len(merged_patterns)} distinct patterns")
        return merged_patterns
    
    def _prepare_time_series(self, metrics: MetricsData) -> pd.DataFrame:
        """Prepare a unified time series DataFrame from all metrics"""
        dfs = []
        
        for metric_type, df in [
            ("cpu", metrics.cpu),
            ("memory", metrics.memory),
            ("replicas", metrics.replicas)
        ]:
            if df.empty:
                continue
            
            # Aggregate by timestamp if multiple series
            agg_df = df.groupby("timestamp").agg({
                "value": ["mean", "max", "min", "std"]
            }).reset_index()
            agg_df.columns = ["timestamp", f"{metric_type}_mean", f"{metric_type}_max", 
                             f"{metric_type}_min", f"{metric_type}_std"]
            dfs.append(agg_df)
        
        if not dfs:
            return pd.DataFrame()
        
        # Merge all metrics on timestamp
        result = dfs[0]
        for df in dfs[1:]:
            result = pd.merge(result, df, on="timestamp", how="outer")
        
        # Fill missing values with forward fill then backward fill
        result = result.sort_values("timestamp")
        result = result.ffill().bfill()
        
        return result
    
    def _extract_time_features(self, df: pd.DataFrame) -> pd.DataFrame:
        """Extract time-based features for pattern detection"""
        df = df.copy()
        
        df["hour"] = df["timestamp"].dt.hour
        df["minute"] = df["timestamp"].dt.minute
        df["day_of_week"] = df["timestamp"].dt.dayofweek
        df["is_weekend"] = df["day_of_week"].isin([5, 6]).astype(int)
        df["time_of_day"] = df["hour"] + df["minute"] / 60.0
        
        # Create time slot (15-minute intervals)
        df["time_slot"] = df["hour"] * 4 + df["minute"] // 15
        
        # Week number for trend analysis
        df["week"] = df["timestamp"].dt.isocalendar().week
        
        return df
    
    def _detect_patterns_with_clustering(self, df: pd.DataFrame) -> List[TimePattern]:
        """Use K-Means clustering to detect patterns"""
        
        # Prepare features for clustering
        feature_cols = []
        for col in ["cpu_mean", "memory_mean", "replicas_mean"]:
            if col in df.columns:
                feature_cols.append(col)
        
        if not feature_cols:
            return []
        
        # Add time features
        feature_cols.extend(["time_of_day", "is_weekend"])
        
        # Group by day and time slot for aggregation
        grouped = df.groupby(["day_of_week", "time_slot"]).agg({
            col: "mean" for col in feature_cols if col in df.columns
        }).reset_index()
        
        if len(grouped) < 3:
            return []
        
        # Scale features
        X = grouped[feature_cols].values
        X_scaled = self.scaler.fit_transform(X)
        
        # Find optimal number of clusters
        best_k, best_score = self._find_optimal_clusters(X_scaled)
        print(f"Optimal clusters: {best_k} (silhouette score: {best_score:.3f})")
        
        # Apply K-Means
        kmeans = KMeans(n_clusters=best_k, random_state=42, n_init=10)
        grouped["cluster"] = kmeans.fit_predict(X_scaled)
        
        # Extract patterns from clusters
        patterns = []
        for cluster_id in range(best_k):
            cluster_data = grouped[grouped["cluster"] == cluster_id]
            pattern = self._extract_pattern_from_cluster(cluster_data, df)
            if pattern:
                patterns.append(pattern)
        
        return patterns
    
    def _find_optimal_clusters(self, X: np.ndarray) -> Tuple[int, float]:
        """Find optimal number of clusters using silhouette score"""
        min_k, max_k = self.config.n_clusters_range
        max_k = min(max_k, len(X) - 1)
        
        if max_k < min_k:
            return min_k, 0.0
        
        best_k = min_k
        best_score = -1
        
        for k in range(min_k, max_k + 1):
            try:
                kmeans = KMeans(n_clusters=k, random_state=42, n_init=10)
                labels = kmeans.fit_predict(X)
                
                if len(set(labels)) < 2:
                    continue
                
                score = silhouette_score(X, labels)
                if score > best_score:
                    best_score = score
                    best_k = k
            except Exception:
                continue
        
        return best_k, best_score
    
    def _extract_pattern_from_cluster(
        self, 
        cluster_data: pd.DataFrame,
        full_df: pd.DataFrame
    ) -> Optional[TimePattern]:
        """Extract a time pattern from cluster data"""
        if cluster_data.empty:
            return None
        
        # Get time range
        time_slots = cluster_data["time_slot"].values
        days = cluster_data["day_of_week"].unique().tolist()
        
        # Convert time slots to hours
        start_slot = int(np.min(time_slots))
        end_slot = int(np.max(time_slots))
        
        start_hour = start_slot // 4
        start_minute = (start_slot % 4) * 15
        end_hour = end_slot // 4
        end_minute = ((end_slot % 4) + 1) * 15
        
        if end_minute >= 60:
            end_hour += 1
            end_minute = 0
        if end_hour >= 24:
            end_hour = 23
            end_minute = 59
        
        # Calculate metrics
        avg_cpu = cluster_data.get("cpu_mean", pd.Series([0])).mean()
        avg_memory = cluster_data.get("memory_mean", pd.Series([0])).mean()
        avg_replicas = cluster_data.get("replicas_mean", pd.Series([1])).mean()
        peak_replicas = int(np.ceil(cluster_data.get("replicas_mean", pd.Series([1])).max()))
        
        # Calculate confidence based on data consistency
        if "cpu_mean" in cluster_data.columns:
            cv = cluster_data["cpu_mean"].std() / (cluster_data["cpu_mean"].mean() + 1e-10)
            confidence = max(0.5, 1.0 - cv)  # Lower variance = higher confidence
        else:
            confidence = 0.7
        
        return TimePattern(
            start_hour=start_hour,
            start_minute=start_minute,
            end_hour=end_hour,
            end_minute=end_minute,
            days=sorted(days),
            avg_cpu=avg_cpu,
            avg_memory=avg_memory,
            avg_replicas=avg_replicas,
            peak_replicas=max(1, peak_replicas),
            confidence=min(1.0, confidence)
        )
    
    def _refine_patterns(
        self, 
        patterns: List[TimePattern], 
        df: pd.DataFrame
    ) -> List[TimePattern]:
        """Refine patterns using statistical analysis"""
        refined = []
        
        for pattern in patterns:
            # Check if pattern spans enough time
            duration_hours = (
                (pattern.end_hour - pattern.start_hour) + 
                (pattern.end_minute - pattern.start_minute) / 60
            )
            
            if duration_hours < self.config.min_pattern_duration_hours:
                continue
            
            # Validate pattern with actual data
            mask = (
                (df["hour"] >= pattern.start_hour) &
                (df["hour"] <= pattern.end_hour) &
                (df["day_of_week"].isin(pattern.days))
            )
            
            matching_data = df[mask]
            if len(matching_data) < 10:  # Need minimum data points
                continue
            
            # Recalculate metrics from actual data
            if "replicas_mean" in matching_data.columns:
                pattern.avg_replicas = matching_data["replicas_mean"].mean()
                pattern.peak_replicas = int(np.ceil(matching_data["replicas_mean"].max()))
            
            refined.append(pattern)
        
        return refined
    
    def _merge_similar_patterns(
        self, 
        patterns: List[TimePattern]
    ) -> List[TimePattern]:
        """Merge patterns that have similar characteristics"""
        if len(patterns) <= 1:
            return patterns
        
        merged = []
        used = set()
        
        for i, p1 in enumerate(patterns):
            if i in used:
                continue
            
            similar = [p1]
            for j, p2 in enumerate(patterns[i+1:], i+1):
                if j in used:
                    continue
                
                # Check if patterns are similar (same days and overlapping times)
                if (set(p1.days) == set(p2.days) and
                    abs(p1.avg_replicas - p2.avg_replicas) < 2):
                    # Check time overlap
                    if (p1.start_hour <= p2.end_hour and p2.start_hour <= p1.end_hour):
                        similar.append(p2)
                        used.add(j)
            
            # Merge similar patterns
            if len(similar) > 1:
                merged_pattern = self._merge_pattern_group(similar)
                merged.append(merged_pattern)
            else:
                merged.append(p1)
            
            used.add(i)
        
        return merged
    
    def _merge_pattern_group(self, patterns: List[TimePattern]) -> TimePattern:
        """Merge a group of similar patterns into one"""
        return TimePattern(
            start_hour=min(p.start_hour for p in patterns),
            start_minute=min(p.start_minute for p in patterns),
            end_hour=max(p.end_hour for p in patterns),
            end_minute=max(p.end_minute for p in patterns),
            days=sorted(set(d for p in patterns for d in p.days)),
            avg_cpu=np.mean([p.avg_cpu for p in patterns]),
            avg_memory=np.mean([p.avg_memory for p in patterns]),
            avg_replicas=np.mean([p.avg_replicas for p in patterns]),
            peak_replicas=max(p.peak_replicas for p in patterns),
            confidence=np.mean([p.confidence for p in patterns])
        )

