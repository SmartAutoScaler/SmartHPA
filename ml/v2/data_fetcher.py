"""
Data Fetcher - Generic Prometheus client for fetching replica metrics
"""

import requests
import numpy as np
from datetime import datetime, timedelta
from typing import Optional, Tuple
from dataclasses import dataclass

from .config import PrometheusConfig, FetchConfig


@dataclass
class ReplicaMetrics:
    """Container for fetched replica metrics"""
    timestamps: np.ndarray  # Unix timestamps
    values: np.ndarray  # Replica counts
    hours: np.ndarray  # Hour of day (0-23)
    days_of_week: np.ndarray  # Day of week (0=Mon, 6=Sun)
    dates: np.ndarray  # Date strings for grouping
    
    @property
    def count(self) -> int:
        return len(self.timestamps)
    
    @property
    def is_empty(self) -> bool:
        return self.count == 0


class PrometheusDataFetcher:
    """
    Fetches replica count metrics from Prometheus.
    
    Workflow:
    1. Build query with labels
    2. Execute range query for 30 days
    3. Parse response into numpy arrays
    4. Return structured ReplicaMetrics
    """
    
    def __init__(self, prom_config: PrometheusConfig):
        self.config = prom_config
        self.session = requests.Session()
    
    def fetch(self, fetch_config: FetchConfig) -> ReplicaMetrics:
        """
        Fetch 30 days of replica metrics at 5-minute intervals.
        
        Args:
            fetch_config: Configuration with namespace, deployment, etc.
            
        Returns:
            ReplicaMetrics with timestamps and values
        """
        end_time = datetime.utcnow()
        start_time = end_time - timedelta(days=fetch_config.days)
        step = f"{fetch_config.step_minutes}m"
        
        # Try primary query first, then fallback
        labels = fetch_config.to_label_selector()
        queries = [
            self.config.replica_query.format(labels=labels),
            self.config.hpa_replica_query.format(labels=labels),
        ]
        
        for query in queries:
            try:
                metrics = self._query_range(query, start_time, end_time, step)
                if not metrics.is_empty:
                    print(f"✓ Fetched {metrics.count} data points from Prometheus")
                    return metrics
            except Exception as e:
                print(f"Query failed: {e}")
                continue
        
        # Generate sample data if Prometheus unavailable
        print("⚠ Prometheus unavailable, generating sample data")
        return self._generate_sample_data(start_time, end_time, fetch_config.step_minutes)
    
    def _query_range(
        self, 
        query: str, 
        start: datetime, 
        end: datetime, 
        step: str
    ) -> ReplicaMetrics:
        """Execute Prometheus range query"""
        url = f"{self.config.url}/api/v1/query_range"
        
        params = {
            "query": query,
            "start": start.isoformat() + "Z",
            "end": end.isoformat() + "Z",
            "step": step
        }
        
        response = self.session.get(url, params=params, timeout=self.config.timeout)
        response.raise_for_status()
        data = response.json()
        
        if data["status"] != "success":
            raise ValueError(f"Query failed: {data.get('error', 'Unknown')}")
        
        return self._parse_response(data["data"]["result"])
    
    def _parse_response(self, result: list) -> ReplicaMetrics:
        """Parse Prometheus response into ReplicaMetrics"""
        if not result:
            return self._empty_metrics()
        
        # Aggregate all series (in case of multiple pods/deployments)
        all_timestamps = []
        all_values = []
        
        for series in result:
            values = series.get("values", [])
            for ts, val in values:
                all_timestamps.append(float(ts))
                try:
                    all_values.append(float(val))
                except ValueError:
                    all_values.append(np.nan)
        
        if not all_timestamps:
            return self._empty_metrics()
        
        # Convert to numpy arrays
        timestamps = np.array(all_timestamps)
        values = np.array(all_values)
        
        # Remove NaN values
        mask = ~np.isnan(values)
        timestamps = timestamps[mask]
        values = values[mask]
        
        # Sort by timestamp
        sort_idx = np.argsort(timestamps)
        timestamps = timestamps[sort_idx]
        values = values[sort_idx]
        
        # If multiple series, aggregate by timestamp (take max replicas)
        unique_ts, indices = np.unique(timestamps, return_inverse=True)
        aggregated = np.zeros(len(unique_ts))
        for i, idx in enumerate(indices):
            aggregated[idx] = max(aggregated[idx], values[i])
        
        timestamps = unique_ts
        values = aggregated
        
        return self._build_metrics(timestamps, values)
    
    def _build_metrics(self, timestamps: np.ndarray, values: np.ndarray) -> ReplicaMetrics:
        """Build ReplicaMetrics with derived fields"""
        # Convert timestamps to datetime components
        dts = [datetime.utcfromtimestamp(ts) for ts in timestamps]
        
        hours = np.array([dt.hour for dt in dts])
        days_of_week = np.array([dt.weekday() for dt in dts])
        dates = np.array([dt.strftime("%Y-%m-%d") for dt in dts])
        
        return ReplicaMetrics(
            timestamps=timestamps,
            values=values,
            hours=hours,
            days_of_week=days_of_week,
            dates=dates
        )
    
    def _empty_metrics(self) -> ReplicaMetrics:
        """Return empty metrics"""
        return ReplicaMetrics(
            timestamps=np.array([]),
            values=np.array([]),
            hours=np.array([]),
            days_of_week=np.array([]),
            dates=np.array([])
        )
    
    def _generate_sample_data(
        self, 
        start: datetime, 
        end: datetime, 
        step_minutes: int
    ) -> ReplicaMetrics:
        """
        Generate realistic sample data for testing.
        
        Patterns:
        - Peak hours (9 AM - 5 PM weekdays): replicas 3-6, burst to 10-15
        - Off-peak (5 PM - 8:59 AM weekdays): replicas 1-3
        - Weekend: replicas 1-3
        """
        np.random.seed(42)
        
        # Generate timestamps
        current = start
        timestamps = []
        while current < end:
            timestamps.append(current.timestamp())
            current += timedelta(minutes=step_minutes)
        
        timestamps = np.array(timestamps)
        n = len(timestamps)
        
        # Generate replica values
        dts = [datetime.utcfromtimestamp(ts) for ts in timestamps]
        values = np.zeros(n)
        
        for i, dt in enumerate(dts):
            hour = dt.hour
            day_of_week = dt.weekday()  # 0=Monday, 6=Sunday
            
            # Weekend - off-peak pattern (replicas 1-3)
            if day_of_week >= 5:
                values[i] = np.random.uniform(1, 3)
            
            # Weekday peak hours (9 AM - 5 PM) - replicas 3-6
            elif 9 <= hour < 17:
                base = np.random.uniform(3, 6)
                # 10% chance of burst to 10-15
                if np.random.random() < 0.10:
                    base = np.random.uniform(10, 15)
                values[i] = base
            
            # Weekday off-peak - replicas 1-3
            else:
                values[i] = np.random.uniform(1, 3)
        
        # Add small noise and round
        noise = np.random.normal(0, 0.3, n)
        values = np.maximum(1, np.round(values + noise))
        
        return self._build_metrics(timestamps, values)

