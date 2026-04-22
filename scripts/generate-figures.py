#!/usr/bin/env -S uv run --quiet
# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "matplotlib>=3.8",
#   "pandas>=2.1",
#   "numpy>=1.26",
# ]
# ///
"""Generate evaluation figures from StarNexus analysis artifacts.

Reads:
  analysis-output/latest/{metrics.csv,events.csv,experiment_evaluation.csv,experiments.jsonl}
  analysis-output/bench/{benchmark.csv,per_experiment.csv}

Writes PNGs to analysis-output/figures/ (or --out).
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path

import matplotlib.pyplot as plt
import numpy as np
import pandas as pd

ROOT = Path(__file__).resolve().parents[1]


def load_experiments(path: Path) -> pd.DataFrame:
    rows = []
    if not path.exists():
        return pd.DataFrame()
    with path.open() as handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            rows.append(json.loads(line))
    return pd.DataFrame(rows)


def fig_metric_timeseries(metrics: pd.DataFrame, experiments: pd.DataFrame, out: Path) -> None:
    if metrics.empty:
        return
    nodes = metrics["node_id"].unique()
    fig, axes = plt.subplots(len(nodes), 1, figsize=(12, 2.6 * len(nodes)), sharex=True)
    if len(nodes) == 1:
        axes = [axes]
    for ax, node_id in zip(axes, sorted(nodes)):
        node_df = metrics[metrics["node_id"] == node_id].sort_values("timestamp")
        ax.plot(pd.to_datetime(node_df["timestamp"], unit="s"), node_df["cpu_percent"],
                color="#1f77b4", linewidth=0.7, label="CPU %")
        node_exp = experiments[experiments["node_id"] == node_id] if not experiments.empty else pd.DataFrame()
        for _, row in node_exp.iterrows():
            ax.axvspan(
                pd.to_datetime(row["started_at"], unit="s"),
                pd.to_datetime(row["ended_at"], unit="s"),
                color="#ff6d64",
                alpha=0.28,
                linewidth=0,
            )
        ax.set_ylabel(f"{node_id}\nCPU %")
        ax.set_ylim(-5, 105)
        ax.grid(True, alpha=0.3)
    axes[0].set_title("CPU time series with labelled fault-injection windows (red)")
    axes[-1].set_xlabel("Time (UTC)")
    fig.tight_layout()
    fig.savefig(out / "cpu_timeseries_with_experiments.png", dpi=140)
    plt.close(fig)


def fig_benchmark_table(bench: pd.DataFrame, out: Path) -> None:
    if bench.empty:
        return
    metrics = [
        ("detection_rate_percent", "Detection rate (%)", False),
        ("mean_detection_delay_seconds", "Mean detection delay (s)", True),
        ("false_positive_rate", "FP events / node-hour", True),
        ("mean_recovery_delay_seconds", "Mean recovery delay (s)", True),
    ]
    fig, axes = plt.subplots(2, 2, figsize=(12, 7))
    for ax, (column, title, lower_is_better) in zip(axes.flat, metrics):
        colors = ["#6fae4e" if lower_is_better else "#1f77b4" for _ in bench["detector"]]
        best_idx = bench[column].idxmin() if lower_is_better else bench[column].idxmax()
        colors = [c for c in colors]
        colors[best_idx] = "#ffba4a"
        bars = ax.bar(bench["detector"], bench[column], color=colors, edgecolor="#333", linewidth=0.7)
        ax.set_title(title)
        ax.grid(True, alpha=0.3, axis="y")
        ax.set_ylim(bottom=0)
        for bar, value in zip(bars, bench[column]):
            ax.text(bar.get_x() + bar.get_width() / 2, bar.get_height(),
                    f"{value:.2f}", ha="center", va="bottom", fontsize=9)
    fig.suptitle("Detector benchmark head-to-head", fontsize=13, fontweight="bold")
    fig.tight_layout()
    fig.savefig(out / "benchmark_head_to_head.png", dpi=140)
    plt.close(fig)


def fig_detection_delay_distribution(per_experiment: pd.DataFrame, out: Path) -> None:
    if per_experiment.empty:
        return
    detectors = sorted(per_experiment["detector"].unique())
    fig, ax = plt.subplots(figsize=(10, 5))
    positions = range(len(detectors))
    data = []
    for detector in detectors:
        sub = per_experiment[(per_experiment["detector"] == detector) & per_experiment["detected"]]
        data.append(sub["detection_delay_seconds"].astype(float).values)
    bp = ax.boxplot(data, positions=positions, widths=0.55, patch_artist=True,
                    showmeans=True, meanline=True,
                    boxprops=dict(facecolor="#cfe3f7", edgecolor="#1f77b4"),
                    medianprops=dict(color="#1f77b4", linewidth=1.5),
                    meanprops=dict(color="#ff6d64", linewidth=1.5, linestyle="--"),
                    whiskerprops=dict(color="#1f77b4"),
                    capprops=dict(color="#1f77b4"))
    for i, values in enumerate(data):
        jitter = np.random.default_rng(42 + i).uniform(-0.15, 0.15, size=len(values))
        ax.scatter([i + j for j in jitter], values, color="#333", alpha=0.6, s=18)
    ax.set_xticks(positions)
    ax.set_xticklabels(detectors, rotation=0)
    ax.set_ylabel("Detection delay (s)")
    ax.set_title("Detection delay distribution per detector (red dashed = mean)")
    ax.grid(True, alpha=0.3, axis="y")
    fig.tight_layout()
    fig.savefig(out / "detection_delay_box.png", dpi=140)
    plt.close(fig)


def fig_delay_vs_duration(per_experiment: pd.DataFrame, out: Path) -> None:
    if per_experiment.empty:
        return
    detected = per_experiment[per_experiment["detected"]].copy()
    if detected.empty:
        return
    detected["duration_seconds"] = detected["duration_seconds"].astype(float)
    detected["detection_delay_seconds"] = detected["detection_delay_seconds"].astype(float)
    fig, ax = plt.subplots(figsize=(10, 5))
    for detector, sub in detected.groupby("detector"):
        means = sub.groupby("duration_seconds")["detection_delay_seconds"].mean()
        ax.plot(means.index, means.values, marker="o", label=detector)
    ax.set_xlabel("Experiment duration (s)")
    ax.set_ylabel("Mean detection delay (s)")
    ax.set_title("Detection delay vs experiment duration")
    ax.grid(True, alpha=0.3)
    ax.legend()
    fig.tight_layout()
    fig.savefig(out / "delay_vs_duration.png", dpi=140)
    plt.close(fig)


def fig_fp_vs_detection_tradeoff(bench: pd.DataFrame, out: Path) -> None:
    if bench.empty:
        return
    fig, ax = plt.subplots(figsize=(8, 6))
    for _, row in bench.iterrows():
        ax.scatter(row["false_positive_rate"], row["detection_rate_percent"],
                   s=220, zorder=3)
        ax.annotate(row["detector"],
                    (row["false_positive_rate"], row["detection_rate_percent"]),
                    xytext=(10, -4),
                    textcoords="offset points",
                    fontsize=10,
                    fontweight="bold")
    ax.set_xscale("symlog", linthresh=0.01)
    ax.set_xlabel("False-positive rate (events per steady-state node-hour, symlog)")
    ax.set_ylabel("Detection rate (%)")
    ax.set_title("FP-rate vs detection-rate tradeoff (top-left is best)")
    ax.grid(True, alpha=0.3)
    ax.set_ylim(-5, 105)
    fig.tight_layout()
    fig.savefig(out / "fp_vs_detection_tradeoff.png", dpi=140)
    plt.close(fig)


def fig_event_severity_timeline(events: pd.DataFrame, experiments: pd.DataFrame, out: Path) -> None:
    if events.empty:
        return
    events = events.copy()
    events["created_at"] = pd.to_datetime(events["created_at"], unit="s")
    fig, ax = plt.subplots(figsize=(12, 4.5))
    palette = {"anomaly": "#ff6d64", "status_change": "#1f77b4"}
    for event_type, color in palette.items():
        sub = events[events["type"] == event_type]
        if sub.empty:
            continue
        ax.scatter(sub["created_at"], sub["node_id"].astype(str),
                   s=60, color=color, alpha=0.7, label=event_type, edgecolor="black", linewidth=0.3)
    if not experiments.empty:
        for _, row in experiments.iterrows():
            ax.axvspan(
                pd.to_datetime(row["started_at"], unit="s"),
                pd.to_datetime(row["ended_at"], unit="s"),
                color="#ffba4a",
                alpha=0.15,
                linewidth=0,
            )
    ax.set_title("Production event timeline (experiment windows shaded)")
    ax.set_ylabel("Node")
    ax.set_xlabel("Time (UTC)")
    ax.legend(loc="upper right")
    ax.grid(True, alpha=0.3)
    fig.tight_layout()
    fig.savefig(out / "event_timeline.png", dpi=140)
    plt.close(fig)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--analysis", default=str(ROOT / "analysis-output" / "latest"),
                        help="Path to analysis export directory (default: latest run)")
    parser.add_argument("--bench", default=str(ROOT / "analysis-output" / "bench"),
                        help="Path to bench output directory")
    parser.add_argument("--out", default=str(ROOT / "analysis-output" / "figures"),
                        help="Output directory for PNGs")
    args = parser.parse_args()

    analysis_dir = Path(args.analysis)
    bench_dir = Path(args.bench)
    out_dir = Path(args.out)
    out_dir.mkdir(parents=True, exist_ok=True)

    metrics_csv = analysis_dir / "metrics.csv"
    events_csv = analysis_dir / "events.csv"
    experiments_jsonl = analysis_dir / "experiments.jsonl"
    bench_csv = bench_dir / "benchmark.csv"
    per_experiment_csv = bench_dir / "per_experiment.csv"

    metrics = pd.read_csv(metrics_csv) if metrics_csv.exists() else pd.DataFrame()
    events = pd.read_csv(events_csv) if events_csv.exists() else pd.DataFrame()
    experiments = load_experiments(experiments_jsonl)
    bench = pd.read_csv(bench_csv) if bench_csv.exists() else pd.DataFrame()
    per_experiment = pd.read_csv(per_experiment_csv) if per_experiment_csv.exists() else pd.DataFrame()

    fig_metric_timeseries(metrics, experiments, out_dir)
    fig_benchmark_table(bench, out_dir)
    fig_detection_delay_distribution(per_experiment, out_dir)
    fig_delay_vs_duration(per_experiment, out_dir)
    fig_fp_vs_detection_tradeoff(bench, out_dir)
    fig_event_severity_timeline(events, experiments, out_dir)

    print(f"Figures written to {out_dir}")
    for png in sorted(out_dir.glob("*.png")):
        print(f"  - {png.name}")


if __name__ == "__main__":
    main()
