"""
Synthetic Data Generator for SmartHPA ML Module

Generates realistic replica count patterns for training and testing:
- Peak hours (9 AM - 5 PM weekdays): replicas 3-6, max 10-15
- Off-peak (5 PM - 8:59 AM weekdays): replicas 1, max 3  
- Weekends: similar to off-peak (replicas 1, max 3)
"""

import numpy as np
import pandas as pd
from datetime import datetime, timedelta
from typing import Tuple, Optional
from dataclasses import dataclass
import json
import os


@dataclass
class SyntheticConfig:
    """Configuration for synthetic data generation"""
    # Time range
    days: int = 30
    step_minutes: int = 5
    
    # Peak hours (weekdays 9 AM - 5 PM)
    peak_start_hour: int = 9
    peak_end_hour: int = 17
    peak_min_replicas: int = 3
    peak_max_replicas: int = 6
    peak_burst_max: int = 15  # Occasional spikes up to 10-15
    peak_burst_probability: float = 0.05  # 5% chance of burst
    
    # Off-peak (weekdays 5 PM - 8:59 AM)
    offpeak_min_replicas: int = 1
    offpeak_max_replicas: int = 3
    
    # Weekend (similar to off-peak)
    weekend_min_replicas: int = 1
    weekend_max_replicas: int = 3
    
    # Noise
    noise_std: float = 0.3
    
    # Random seed for reproducibility
    seed: Optional[int] = 42


class SyntheticDataGenerator:
    """
    Generates synthetic replica count data with realistic patterns.
    
    Patterns:
    - Peak: 9 AM - 5 PM weekdays, replicas 3-6 (bursts to 10-15)
    - Off-peak: 5 PM - 8:59 AM weekdays, replicas 1-3
    - Weekend: All day Sat/Sun, replicas 1-3
    """
    
    def __init__(self, config: SyntheticConfig = None):
        self.config = config or SyntheticConfig()
        if self.config.seed is not None:
            np.random.seed(self.config.seed)
    
    def generate(self, start_date: datetime = None) -> Tuple[np.ndarray, np.ndarray, pd.DataFrame]:
        """
        Generate synthetic replica data.
        
        Args:
            start_date: Start date for data (default: 30 days ago)
            
        Returns:
            Tuple of (timestamps, replica_values, dataframe)
        """
        if start_date is None:
            start_date = datetime.utcnow() - timedelta(days=self.config.days)
        
        end_date = start_date + timedelta(days=self.config.days)
        
        # Generate timestamps at 5-minute intervals
        timestamps = []
        current = start_date
        while current < end_date:
            timestamps.append(current)
            current += timedelta(minutes=self.config.step_minutes)
        
        timestamps = np.array(timestamps)
        n = len(timestamps)
        
        # Generate replica values based on patterns
        replicas = np.zeros(n)
        
        for i, ts in enumerate(timestamps):
            replicas[i] = self._get_replica_count(ts)
        
        # Create DataFrame for easy analysis
        df = pd.DataFrame({
            'timestamp': timestamps,
            'replicas': replicas,
            'hour': [ts.hour for ts in timestamps],
            'day_of_week': [ts.weekday() for ts in timestamps],
            'is_weekend': [ts.weekday() >= 5 for ts in timestamps],
            'is_peak': [self._is_peak_hour(ts) for ts in timestamps]
        })
        
        # Convert timestamps to unix for model compatibility
        unix_timestamps = np.array([ts.timestamp() for ts in timestamps])
        
        print(f"✓ Generated {n} data points ({self.config.days} days)")
        print(f"  Peak hours: {self.config.peak_start_hour}:00 - {self.config.peak_end_hour}:00")
        print(f"  Peak replicas: {self.config.peak_min_replicas}-{self.config.peak_max_replicas} (burst to {self.config.peak_burst_max})")
        print(f"  Off-peak/Weekend replicas: {self.config.offpeak_min_replicas}-{self.config.offpeak_max_replicas}")
        
        return unix_timestamps, replicas, df
    
    def _get_replica_count(self, ts: datetime) -> int:
        """Determine replica count based on time"""
        hour = ts.hour
        day_of_week = ts.weekday()  # 0=Monday, 6=Sunday
        
        # Weekend - off-peak pattern
        if day_of_week >= 5:  # Saturday or Sunday
            base = np.random.uniform(
                self.config.weekend_min_replicas,
                self.config.weekend_max_replicas
            )
            noise = np.random.normal(0, self.config.noise_std)
            return max(1, int(round(base + noise)))
        
        # Weekday
        if self._is_peak_hour(ts):
            # Peak hours (9 AM - 5 PM)
            base = np.random.uniform(
                self.config.peak_min_replicas,
                self.config.peak_max_replicas
            )
            
            # Random bursts during peak
            if np.random.random() < self.config.peak_burst_probability:
                base = np.random.uniform(10, self.config.peak_burst_max)
            
            noise = np.random.normal(0, self.config.noise_std)
            return max(self.config.peak_min_replicas, int(round(base + noise)))
        else:
            # Off-peak hours
            base = np.random.uniform(
                self.config.offpeak_min_replicas,
                self.config.offpeak_max_replicas
            )
            noise = np.random.normal(0, self.config.noise_std)
            return max(1, int(round(base + noise)))
    
    def _is_peak_hour(self, ts: datetime) -> bool:
        """Check if timestamp is during peak hours"""
        return (ts.weekday() < 5 and 
                self.config.peak_start_hour <= ts.hour < self.config.peak_end_hour)
    
    def generate_training_set(self, num_samples: int = 5) -> list:
        """
        Generate multiple training samples with slight variations.
        
        Args:
            num_samples: Number of training samples to generate
            
        Returns:
            List of (timestamps, replicas, df) tuples
        """
        samples = []
        
        for i in range(num_samples):
            # Vary the seed for each sample
            np.random.seed(self.config.seed + i if self.config.seed else None)
            
            # Slight variations in config
            config = SyntheticConfig(
                days=self.config.days,
                step_minutes=self.config.step_minutes,
                peak_min_replicas=self.config.peak_min_replicas + np.random.randint(-1, 2),
                peak_max_replicas=self.config.peak_max_replicas + np.random.randint(-1, 2),
                peak_burst_max=self.config.peak_burst_max + np.random.randint(-2, 3),
                offpeak_min_replicas=self.config.offpeak_min_replicas,
                offpeak_max_replicas=self.config.offpeak_max_replicas,
                seed=self.config.seed + i if self.config.seed else None
            )
            
            generator = SyntheticDataGenerator(config)
            start_date = datetime.utcnow() - timedelta(days=config.days + i * 7)
            samples.append(generator.generate(start_date))
        
        return samples
    
    def save_to_csv(self, filepath: str) -> pd.DataFrame:
        """Generate and save data to CSV"""
        _, _, df = self.generate()
        df.to_csv(filepath, index=False)
        print(f"Saved to {filepath}")
        return df
    
    def save_to_json(self, filepath: str) -> dict:
        """Generate and save data to JSON (Prometheus-like format)"""
        timestamps, replicas, _ = self.generate()
        
        data = {
            "status": "success",
            "data": {
                "resultType": "matrix",
                "result": [{
                    "metric": {
                        "deployment": "synthetic-app",
                        "namespace": "synthetic-namespace"
                    },
                    "values": [
                        [int(ts), str(int(r))] 
                        for ts, r in zip(timestamps, replicas)
                    ]
                }]
            }
        }
        
        with open(filepath, 'w') as f:
            json.dump(data, f)
        
        print(f"Saved to {filepath}")
        return data
    
    def get_expected_patterns(self) -> dict:
        """Return the expected patterns for validation"""
        return {
            "peak": {
                "hours": list(range(self.config.peak_start_hour, self.config.peak_end_hour)),
                "days": [0, 1, 2, 3, 4],  # Mon-Fri
                "min_replicas": self.config.peak_min_replicas,
                "max_replicas": self.config.peak_burst_max,
                "avg_replicas": (self.config.peak_min_replicas + self.config.peak_max_replicas) / 2
            },
            "off_peak": {
                "hours": list(range(0, self.config.peak_start_hour)) + 
                        list(range(self.config.peak_end_hour, 24)),
                "days": [0, 1, 2, 3, 4],  # Mon-Fri
                "min_replicas": self.config.offpeak_min_replicas,
                "max_replicas": self.config.offpeak_max_replicas,
                "avg_replicas": (self.config.offpeak_min_replicas + self.config.offpeak_max_replicas) / 2
            },
            "weekend": {
                "hours": list(range(24)),
                "days": [5, 6],  # Sat-Sun
                "min_replicas": self.config.weekend_min_replicas,
                "max_replicas": self.config.weekend_max_replicas,
                "avg_replicas": (self.config.weekend_min_replicas + self.config.weekend_max_replicas) / 2
            }
        }


def generate_and_test():
    """Generate synthetic data and test with the ML pipeline"""
    print("=" * 60)
    print("Synthetic Data Generator")
    print("=" * 60)
    
    # Create generator with user-specified patterns
    config = SyntheticConfig(
        days=30,
        step_minutes=5,
        peak_start_hour=9,
        peak_end_hour=17,
        peak_min_replicas=3,
        peak_max_replicas=6,
        peak_burst_max=15,
        offpeak_min_replicas=1,
        offpeak_max_replicas=3,
        weekend_min_replicas=1,
        weekend_max_replicas=3,
        seed=42
    )
    
    generator = SyntheticDataGenerator(config)
    timestamps, replicas, df = generator.generate()
    
    # Show statistics
    print("\n" + "=" * 60)
    print("Data Statistics")
    print("=" * 60)
    
    # Peak hours stats
    peak_data = df[df['is_peak']]
    offpeak_data = df[(~df['is_peak']) & (~df['is_weekend'])]
    weekend_data = df[df['is_weekend']]
    
    print(f"\nPeak Hours (9 AM - 5 PM weekdays):")
    print(f"  Count: {len(peak_data)}")
    print(f"  Avg replicas: {peak_data['replicas'].mean():.2f}")
    print(f"  Min: {peak_data['replicas'].min()}, Max: {peak_data['replicas'].max()}")
    
    print(f"\nOff-Peak (5 PM - 9 AM weekdays):")
    print(f"  Count: {len(offpeak_data)}")
    print(f"  Avg replicas: {offpeak_data['replicas'].mean():.2f}")
    print(f"  Min: {offpeak_data['replicas'].min()}, Max: {offpeak_data['replicas'].max()}")
    
    print(f"\nWeekend:")
    print(f"  Count: {len(weekend_data)}")
    print(f"  Avg replicas: {weekend_data['replicas'].mean():.2f}")
    print(f"  Min: {weekend_data['replicas'].min()}, Max: {weekend_data['replicas'].max()}")
    
    # Expected patterns
    print("\n" + "=" * 60)
    print("Expected Patterns (Ground Truth)")
    print("=" * 60)
    expected = generator.get_expected_patterns()
    for name, pattern in expected.items():
        print(f"\n{name.upper()}:")
        print(f"  Hours: {pattern['hours'][0]}:00 - {pattern['hours'][-1]+1}:00")
        print(f"  Days: {['Mon','Tue','Wed','Thu','Fri','Sat','Sun'][pattern['days'][0]]} - "
              f"{['Mon','Tue','Wed','Thu','Fri','Sat','Sun'][pattern['days'][-1]]}")
        print(f"  Replicas: {pattern['min_replicas']}-{pattern['max_replicas']}")
    
    return timestamps, replicas, df, generator


if __name__ == "__main__":
    timestamps, replicas, df, generator = generate_and_test()
    
    # Save sample data
    script_dir = os.path.dirname(os.path.abspath(__file__))
    generator.save_to_csv(os.path.join(script_dir, "synthetic_data.csv"))



