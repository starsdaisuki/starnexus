#!/usr/bin/env -S uv run --quiet
# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "pandas>=2.1",
#   "numpy>=1.26",
# ]
# ///
"""Weight-sensitivity analysis for the reliability composite score.

The production weights are Availability 40%, Latency 30%, Stability 30%.
These were chosen for interpretability; this script sweeps a small grid
of alternative weight schemes and reports the resulting per-node scores
and rankings, plus Kendall-tau agreement between schemes.

Small-N note: the current fleet has 3 nodes, so Kendall-tau is either
+1, 0, or -1 across most comparisons. The real value of this script is
demonstrating that the default weights produce a ranking that is
stable under reasonable perturbation, which is the defensible claim we
can make at this sample size.

Usage:
  scripts/validate-reliability.py --db backups/starnexus-db-*.sqlite
  scripts/validate-reliability.py --db server/starnexus.db --out analysis-output/weight-sensitivity.csv
"""
from __future__ import annotations

import argparse
import itertools
import math
import sqlite3
from pathlib import Path

import numpy as np
import pandas as pd


SCHEMES = {
    "default (40/30/30)":  (0.40, 0.30, 0.30),
    "heavy-availability (60/20/20)": (0.60, 0.20, 0.20),
    "balanced (33/33/33)": (1 / 3, 1 / 3, 1 / 3),
    "latency-focused (30/50/20)": (0.30, 0.50, 0.20),
    "stability-focused (30/20/50)": (0.30, 0.20, 0.50),
}


def load_scores(db_path: Path) -> pd.DataFrame:
    conn = sqlite3.connect(str(db_path))
    try:
        rows = conn.execute(
            "SELECT node_id, availability, latency_score, stability, composite_score, updated_at FROM node_scores"
        ).fetchall()
        columns = ["node_id", "availability", "latency_score", "stability", "composite_score", "updated_at"]
    finally:
        conn.close()
    return pd.DataFrame(rows, columns=columns)


def composite(row: pd.Series, weights: tuple[float, float, float]) -> float:
    a, l, s = weights
    return a * row["availability"] + l * row["latency_score"] + s * row["stability"]


def kendall_tau(ordered_a: list[str], ordered_b: list[str]) -> float:
    pairs = list(itertools.combinations(range(len(ordered_a)), 2))
    if not pairs:
        return math.nan
    concordant = 0
    discordant = 0
    rank_b = {node: idx for idx, node in enumerate(ordered_b)}
    for i, j in pairs:
        ni, nj = ordered_a[i], ordered_a[j]
        if rank_b[ni] < rank_b[nj]:
            concordant += 1
        else:
            discordant += 1
    total = concordant + discordant
    if total == 0:
        return math.nan
    return (concordant - discordant) / total


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--db", required=True, help="Path to SQLite DB containing node_scores")
    parser.add_argument("--out", default="", help="Optional CSV output path")
    args = parser.parse_args()

    scores = load_scores(Path(args.db))
    if scores.empty:
        raise SystemExit("no rows in node_scores — run the scoring scheduler first")

    result = scores[["node_id", "availability", "latency_score", "stability"]].copy()
    for scheme, weights in SCHEMES.items():
        result[scheme] = scores.apply(lambda row: composite(row, weights), axis=1)

    print("Per-node scores across weight schemes:")
    print(result.round(2).to_string(index=False))

    print("\nRanking stability (Kendall-tau vs default):")
    default_ranking = result.sort_values(SCHEMES_KEY_DEFAULT, ascending=False)["node_id"].tolist()
    print(f"  default ranking: {default_ranking}")
    for scheme in SCHEMES:
        if scheme == SCHEMES_KEY_DEFAULT:
            continue
        ranking = result.sort_values(scheme, ascending=False)["node_id"].tolist()
        tau = kendall_tau(default_ranking, ranking)
        print(f"  vs {scheme}: {ranking} (tau = {tau:+.2f})")

    spread = result[list(SCHEMES)].max(axis=1) - result[list(SCHEMES)].min(axis=1)
    print("\nPer-node composite score range across schemes (max - min):")
    for _, row in result.assign(spread=spread).iterrows():
        print(f"  {row['node_id']}: {row['spread']:.2f}")

    if args.out:
        out_path = Path(args.out)
        out_path.parent.mkdir(parents=True, exist_ok=True)
        result.to_csv(out_path, index=False)
        print(f"\nSaved per-node sensitivity table to {out_path}")


SCHEMES_KEY_DEFAULT = "default (40/30/30)"


if __name__ == "__main__":
    main()
