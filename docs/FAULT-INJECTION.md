# Controlled Fault Injection

Fault injection is used to create labelled abnormal intervals for evaluating anomaly detection. Do not run these experiments on a node that is actively serving important traffic.

## Safe Default Experiment

The repository includes a CPU-only experiment wrapper:

```bash
scripts/fault-injection.sh --ssh-host lisahost --node-id jp-lisahost --duration 150
```

Safety constraints:

- Uses `nice -n 10` so normal services keep priority.
- Uses `timeout`, so the pressure process exits automatically.
- Does not touch memory pressure, network shaping, firewall rules, proxy services, or SSH settings.
- Polls StarNexus every 30 seconds and writes a CSV log under `analysis-output/`.
- Appends a JSONL ground-truth label to `analysis-output/experiments.jsonl`.
- Also appends that label to `/root/starnexus/analysis-output/experiments.jsonl` on the StarNexus server by default, so the dashboard Experiment View updates automatically.

The JSONL label records `experiment_id`, `node_id`, `injection_type`, `expected_metric`, `started_at`, `ended_at`, and `duration_seconds`. Pass it to the analysis CLI:

```bash
./starnexus-analyze \
  -db ./starnexus.db \
  -schema ./schema.sql \
  -out ./analysis-output/with-experiments \
  -hours 24 \
  -experiments ./analysis-output/experiments.jsonl
```

## What To Measure

For each experiment, record:

- Start timestamp.
- End timestamp.
- Target node id.
- Injection type.
- Expected affected metric.
- First dashboard detection timestamp.
- First recovery timestamp.

The analysis target is:

- Detection delay: first detection minus injection start.
- Recovery delay: first recovery minus injection end.
- False positives: flagged intervals outside known experiment windows.
- Stability: whether the risk level flaps after recovery.

## Production Notes

CPU-only experiments are acceptable on a spare node. Network experiments are more dangerous because a wrong `tc netem` rule can break SSH or proxy traffic. If network experiments are added later, they should only target a controlled test port or a disposable test node.
