"""
Prometheus Metrics Client for SmartHPA ML Module
Fetches CPU, Memory, and Replica metrics from Prometheus
"""

import requests
import pandas as pd
import numpy as np
from datetime import datetime, timedelta
from typing import Dict, List, Optional, Tuple
from dataclasses import dataclass

try:
    from .config import PrometheusConfig, MetricLabels
except ImportError:
    from config import PrometheusConfig, MetricLabels


@dataclass
class MetricsData:
    """Container for fetched metrics data"""
    cpu: pd.DataFrame
    memory: pd.DataFrame
    replicas: pd.DataFrame
    timestamps: pd.DatetimeIndex
    labels: Dict[str, str]


class PrometheusMetricsClient:
    """
    Client for fetching metrics from Prometheus.
    Retrieves CPU, Memory, and Replica count metrics for ML analysis.
    """
    
    def __init__(self, config: PrometheusConfig):
        self.config = config
        self.session = requests.Session()
        if config.auth_token:
            self.session.headers["Authorization"] = f"Bearer {config.auth_token}"
    
    def _query_range(
        self, 
        query: str, 
        start: datetime, 
        end: datetime, 
        step: str = "5m"
    ) -> pd.DataFrame:
        """Execute a range query against Prometheus"""
        url = f"{self.config.url}/api/v1/query_range"
        
        params = {
            "query": query,
            "start": start.isoformat() + "Z",
            "end": end.isoformat() + "Z",
            "step": step
        }
        
        try:
            response = self.session.get(
                url, 
                params=params, 
                timeout=self.config.timeout,
                verify=self.config.verify_ssl
            )
            response.raise_for_status()
            data = response.json()
            
            if data["status"] != "success":
                raise ValueError(f"Prometheus query failed: {data.get('error', 'Unknown error')}")
            
            return self._parse_range_result(data["data"]["result"])
            
        except requests.exceptions.RequestException as e:
            raise ConnectionError(f"Failed to query Prometheus: {e}")
    
    def _parse_range_result(self, result: List[dict]) -> pd.DataFrame:
        """Parse Prometheus range query result into DataFrame"""
        if not result:
            return pd.DataFrame()
        
        all_series = []
        for series in result:
            labels = series.get("metric", {})
            values = series.get("values", [])
            
            if not values:
                continue
            
            timestamps = [datetime.fromtimestamp(v[0]) for v in values]
            data_values = [float(v[1]) if v[1] != "NaN" else np.nan for v in values]
            
            # Create a unique series identifier
            series_id = "_".join([f"{k}={v}" for k, v in sorted(labels.items())])
            
            df = pd.DataFrame({
                "timestamp": timestamps,
                "value": data_values,
                "series_id": series_id
            })
            
            # Add label columns
            for k, v in labels.items():
                df[k] = v
            
            all_series.append(df)
        
        if not all_series:
            return pd.DataFrame()
        
        return pd.concat(all_series, ignore_index=True)
    
    def fetch_cpu_metrics(
        self, 
        labels: MetricLabels, 
        days: int = 30,
        step: str = "5m"
    ) -> pd.DataFrame:
        """
        Fetch CPU utilization metrics.
        Uses container_cpu_usage_seconds_total or node_cpu metrics.
        """
        end = datetime.utcnow()
        start = end - timedelta(days=days)
        
        label_selector = labels.to_label_selector()
        
        # CPU usage rate (cores)
        queries = [
            # Container CPU usage rate
            f'sum(rate(container_cpu_usage_seconds_total{{{label_selector}}}[5m])) by (namespace, pod, container)',
            # Alternative: kube-state-metrics based
            f'sum(rate(container_cpu_usage_seconds_total{{{label_selector}}}[5m])) by (namespace)',
        ]
        
        for query in queries:
            try:
                df = self._query_range(query, start, end, step)
                if not df.empty:
                    df["metric_type"] = "cpu"
                    return df
            except Exception:
                continue
        
        # Fallback: generate sample data if Prometheus unavailable
        print("Warning: Using sample CPU data (Prometheus unavailable)")
        return self._generate_sample_data(start, end, step, "cpu")
    
    def fetch_memory_metrics(
        self, 
        labels: MetricLabels, 
        days: int = 30,
        step: str = "5m"
    ) -> pd.DataFrame:
        """
        Fetch Memory utilization metrics.
        Uses container_memory_usage_bytes or similar metrics.
        """
        end = datetime.utcnow()
        start = end - timedelta(days=days)
        
        label_selector = labels.to_label_selector()
        
        # Memory usage (bytes)
        queries = [
            # Container memory usage
            f'sum(container_memory_usage_bytes{{{label_selector}}}) by (namespace, pod, container)',
            # Working set bytes (excludes cache)
            f'sum(container_memory_working_set_bytes{{{label_selector}}}) by (namespace, pod, container)',
        ]
        
        for query in queries:
            try:
                df = self._query_range(query, start, end, step)
                if not df.empty:
                    df["metric_type"] = "memory"
                    return df
            except Exception:
                continue
        
        print("Warning: Using sample Memory data (Prometheus unavailable)")
        return self._generate_sample_data(start, end, step, "memory")
    
    def fetch_replica_metrics(
        self, 
        labels: MetricLabels, 
        days: int = 30,
        step: str = "5m"
    ) -> pd.DataFrame:
        """
        Fetch replica count metrics.
        Uses kube_deployment_status_replicas or HPA metrics.
        """
        end = datetime.utcnow()
        start = end - timedelta(days=days)
        
        label_selector = labels.to_label_selector()
        
        queries = [
            # Deployment replicas
            f'kube_deployment_status_replicas{{{label_selector}}}',
            # HPA current replicas
            f'kube_horizontalpodautoscaler_status_current_replicas{{{label_selector}}}',
            # Ready replicas
            f'kube_deployment_status_replicas_ready{{{label_selector}}}',
        ]
        
        for query in queries:
            try:
                df = self._query_range(query, start, end, step)
                if not df.empty:
                    df["metric_type"] = "replicas"
                    return df
            except Exception:
                continue
        
        print("Warning: Using sample Replica data (Prometheus unavailable)")
        return self._generate_sample_data(start, end, step, "replicas")
    
    def fetch_all_metrics(
        self, 
        labels: MetricLabels, 
        days: int = 30,
        step: str = "5m"
    ) -> MetricsData:
        """Fetch all metrics (CPU, Memory, Replicas) for analysis"""
        print(f"Fetching {days} days of metrics...")
        
        cpu_df = self.fetch_cpu_metrics(labels, days, step)
        memory_df = self.fetch_memory_metrics(labels, days, step)
        replicas_df = self.fetch_replica_metrics(labels, days, step)
        
        # Get unified timestamps
        all_timestamps = set()
        for df in [cpu_df, memory_df, replicas_df]:
            if not df.empty and "timestamp" in df.columns:
                all_timestamps.update(df["timestamp"].tolist())
        
        timestamps = pd.DatetimeIndex(sorted(all_timestamps))
        
        return MetricsData(
            cpu=cpu_df,
            memory=memory_df,
            replicas=replicas_df,
            timestamps=timestamps,
            labels=labels.custom_labels
        )
    
    def _generate_sample_data(
        self, 
        start: datetime, 
        end: datetime, 
        step: str,
        metric_type: str
    ) -> pd.DataFrame:
        """Generate realistic sample data for testing when Prometheus is unavailable"""
        # Parse step to minutes
        step_minutes = int(step.replace("m", "").replace("h", "")) if "m" in step else 60
        
        timestamps = pd.date_range(start=start, end=end, freq=f"{step_minutes}min")
        
        np.random.seed(42)  # Reproducible results
        
        # Generate realistic patterns
        hour_of_day = np.array([t.hour for t in timestamps])
        day_of_week = np.array([t.weekday() for t in timestamps])
        
        # Base pattern: higher during business hours (9-17), lower on weekends
        business_hours = ((hour_of_day >= 9) & (hour_of_day <= 17)).astype(float)
        weekday = (day_of_week < 5).astype(float)
        
        if metric_type == "cpu":
            # CPU: 0.5-4 cores, higher during business hours
            base = 1.0
            pattern = base + 2.0 * business_hours * weekday + 0.5 * weekday
            noise = np.random.normal(0, 0.2, len(timestamps))
            values = np.maximum(0.1, pattern + noise)
            
        elif metric_type == "memory":
            # Memory: 500MB - 4GB
            base = 1e9  # 1GB base
            pattern = base + 2e9 * business_hours * weekday + 0.5e9 * weekday
            noise = np.random.normal(0, 0.2e9, len(timestamps))
            values = np.maximum(0.5e9, pattern + noise)
            
        else:  # replicas
            # Replicas: 2-10, higher during business hours
            base = 2
            pattern = base + 5 * business_hours * weekday + 2 * weekday
            noise = np.random.normal(0, 0.5, len(timestamps))
            values = np.maximum(1, np.round(pattern + noise))
        
        return pd.DataFrame({
            "timestamp": timestamps,
            "value": values,
            "metric_type": metric_type,
            "series_id": f"sample_{metric_type}",
            "namespace": "default"
        })

