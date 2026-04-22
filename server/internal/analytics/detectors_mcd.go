package analytics

import (
	"fmt"
	"math"
	"slices"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

// ----- Detector 6: MCD-Mahalanobis (full covariance, concentration step) -----

// MCDMahalanobisDetector is the principled upgrade over the diagonal
// MahalanobisDetector. It fits a robust mean and full covariance matrix
// using a simplified Fast-MCD scheme (concentration step on an h-subset
// of the rolling window), then scores each incoming point by the squared
// Mahalanobis distance under the inverse covariance.
//
// Unlike the diagonal variant, this detector captures cross-metric
// correlation: two metrics that co-move in an individually unremarkable
// way but jointly unusual do fire here.
//
// Fast-MCD in one paragraph: given a rolling window of n points in R^p,
// pick h = ⌈α·n⌉ (α≈0.75 by default) of the most central points as the
// robust support. We approximate FastMCD's multi-start + C-step loop
// with a single-start, multi-iteration concentration on the p-dimensional
// median seed. Empirically this converges on VPS load data in 2-3 steps
// and is two orders of magnitude cheaper than full FastMCD — the latter
// is overkill for p=4 and n≤400.
//
// Chi-squared threshold: d² ~ χ²_{p} under the null. For p=4,
// χ²_{0.995, 4} ≈ 14.86, so a distance of √14.86 ≈ 3.86 corresponds to a
// 0.5% per-sample false-positive rate. We default to 4.2 (≈ χ²_{0.998}),
// matching the diagonal variant for head-to-head comparability.
//
// Pressure gate: we only fire when the current vector is above the robust
// centre in at least MinComponentHits dimensions. This keeps the detector
// from flagging "all metrics dropped to zero" which is better handled by
// the status-change pipeline.
type MCDMahalanobisDetector struct {
	Window           int
	RefitEvery       int
	Support          float64 // h/n; 0.5 ≤ Support ≤ 1.0
	Threshold        float64 // Mahalanobis distance (sqrt of squared form)
	MinCurrents      map[string]float64
	MinComponentHits int
	MinHold          int
}

func NewMCDMahalanobisDetector() *MCDMahalanobisDetector {
	return &MCDMahalanobisDetector{
		Window:     200,
		RefitEvery: 30,
		Support:    0.75,
		Threshold:  4.2,
		MinCurrents: map[string]float64{
			"cpu_percent":    40,
			"memory_percent": 50,
			"bandwidth_down": 512,
			"connections":    60,
		},
		MinComponentHits: 1,
		MinHold:          2,
	}
}

func (d *MCDMahalanobisDetector) Name() string { return "mcd_mahalanobis" }
func (d *MCDMahalanobisDetector) Description() string {
	return "Multivariate Mahalanobis with MCD-style robust full covariance (h=0.75·window, 3-step concentration). Captures cross-metric correlation unlike diagonal variant."
}

func (d *MCDMahalanobisDetector) Process(nodeID string, points []db.MetricPoint) []SyntheticEvent {
	if len(points) == 0 {
		return nil
	}
	selectors := defaultMetricSelectors()
	p := len(selectors)
	series := make([][]float64, p)
	for i, sel := range selectors {
		series[i] = make([]float64, len(points))
		for j, point := range points {
			series[i][j] = sel.Extract(point)
		}
	}

	var (
		firing       bool
		consecutive  int
		events       []SyntheticEvent
		cachedMu     []float64
		cachedInv    [][]float64
		cachedAtIdx  int
		cachedValid  bool
		samplesSince int
	)

	const minWindow = 40

	for idx := range points {
		start := idx - d.Window
		if start < 0 {
			start = 0
		}
		if idx-start < minWindow {
			continue
		}

		needRefit := !cachedValid || samplesSince >= d.RefitEvery || idx-cachedAtIdx >= d.RefitEvery
		if needRefit {
			window := make([][]float64, idx-start)
			for j := start; j < idx; j++ {
				row := make([]float64, p)
				for k := 0; k < p; k++ {
					row[k] = series[k][j]
				}
				window[j-start] = row
			}
			mu, inv, ok := fitMCDRobustCov(window, d.Support)
			if ok {
				cachedMu = mu
				cachedInv = inv
				cachedValid = true
				cachedAtIdx = idx
				samplesSince = 0
			} else {
				cachedValid = false
			}
		}

		if !cachedValid {
			samplesSince++
			continue
		}
		samplesSince++

		current := make([]float64, p)
		for k := 0; k < p; k++ {
			current[k] = series[k][idx]
		}

		hits := 0
		primaryMetric := ""
		primaryDelta := 0.0
		for k := 0; k < p; k++ {
			floor := d.MinCurrents[selectors[k].Key]
			if current[k] < floor {
				continue
			}
			// Pressure gate: only count the component toward `hits` if
			// the point is above the robust centre in that dimension.
			// Covariance magnitude is captured by the squared-Mahalanobis
			// distance below — here we only need a direction check.
			delta := current[k] - cachedMu[k]
			if delta <= 0 {
				continue
			}
			hits++
			if primaryMetric == "" || delta > primaryDelta {
				primaryMetric = selectors[k].Label
				primaryDelta = delta
			}
		}

		d2 := squaredMahalanobis(current, cachedMu, cachedInv)
		triggered := hits >= d.MinComponentHits && d2 >= d.Threshold*d.Threshold
		if triggered {
			consecutive++
		} else {
			consecutive = 0
		}

		ts := points[idx].Timestamp
		dist := math.Sqrt(math.Max(d2, 0))
		if !firing && consecutive >= d.MinHold {
			label := primaryMetric
			if label == "" {
				label = "composite"
			}
			events = append(events, SyntheticEvent{
				NodeID:    nodeID,
				Type:      "anomaly",
				Severity:  "warning",
				Timestamp: ts,
				Title:     fmt.Sprintf("%s outlier detected", label),
				Body:      fmt.Sprintf("MCD Mahalanobis d=%.2f (threshold %.2f) across %d dimension(s)", dist, d.Threshold, hits),
				Metric:    "multivariate",
				Value:     dist,
			})
			firing = true
		} else if firing && consecutive == 0 {
			events = append(events, SyntheticEvent{
				NodeID:    nodeID,
				Type:      "status_change",
				Severity:  "info",
				Timestamp: ts,
				Title:     "Node recovered",
				Body:      "MCD Mahalanobis distance returned below threshold",
				Metric:    "multivariate",
				Value:     dist,
			})
			firing = false
		}
	}
	return events
}

// fitMCDRobustCov runs a simplified MCD-style concentration step to obtain
// a robust mean and inverse covariance on an h-subset of `data`. Returns
// the robust centre, the inverse covariance, and a validity flag that is
// false iff the covariance was singular and could not be inverted.
func fitMCDRobustCov(data [][]float64, support float64) ([]float64, [][]float64, bool) {
	n := len(data)
	if n == 0 {
		return nil, nil, false
	}
	p := len(data[0])
	if p == 0 {
		return nil, nil, false
	}
	if support < 0.5 {
		support = 0.5
	}
	if support > 1.0 {
		support = 1.0
	}
	h := int(math.Ceil(support * float64(n)))
	if h < p+1 {
		h = p + 1
	}
	if h > n {
		h = n
	}

	// Seed the concentration with the coordinate-wise median — cheap,
	// deterministic, and already robust.
	seed := make([]float64, p)
	for k := 0; k < p; k++ {
		col := make([]float64, n)
		for j := 0; j < n; j++ {
			col[j] = data[j][k]
		}
		slices.Sort(col)
		seed[k] = percentile(col, 50)
	}

	mu := seed
	var cov [][]float64
	var inv [][]float64

	for iter := 0; iter < 3; iter++ {
		// Rank points by squared Euclidean distance from the current
		// centre weighted by per-dimension scale, picking the h points
		// closest to mu as the support.
		type indexed struct {
			idx  int
			dist float64
		}
		scale := make([]float64, p)
		for k := 0; k < p; k++ {
			col := make([]float64, n)
			for j := 0; j < n; j++ {
				col[j] = data[j][k]
			}
			slices.Sort(col)
			med := percentile(col, 50)
			m := mad(col, med)
			if m < 1e-6 {
				m = 1.0
			}
			scale[k] = 1.4826 * m
		}
		ranked := make([]indexed, n)
		for j := 0; j < n; j++ {
			var dsq float64
			for k := 0; k < p; k++ {
				diff := (data[j][k] - mu[k]) / scale[k]
				dsq += diff * diff
			}
			ranked[j] = indexed{idx: j, dist: dsq}
		}
		slices.SortFunc(ranked, func(a, b indexed) int {
			if a.dist < b.dist {
				return -1
			}
			if a.dist > b.dist {
				return 1
			}
			return 0
		})

		// Recompute mean and covariance on the h-subset.
		newMu := make([]float64, p)
		for i := 0; i < h; i++ {
			row := data[ranked[i].idx]
			for k := 0; k < p; k++ {
				newMu[k] += row[k]
			}
		}
		for k := 0; k < p; k++ {
			newMu[k] /= float64(h)
		}
		newCov := makeMatrix(p, p)
		for i := 0; i < h; i++ {
			row := data[ranked[i].idx]
			for a := 0; a < p; a++ {
				for b := 0; b < p; b++ {
					newCov[a][b] += (row[a] - newMu[a]) * (row[b] - newMu[b])
				}
			}
		}
		for a := 0; a < p; a++ {
			for b := 0; b < p; b++ {
				newCov[a][b] /= float64(h - 1)
			}
		}

		mu = newMu
		cov = newCov

		// Regularise the diagonal slightly to avoid singularities on
		// near-constant metrics (e.g. always-idle memory).
		for a := 0; a < p; a++ {
			if cov[a][a] < 1e-6 {
				cov[a][a] = 1e-6
			}
		}

		// Trace-proportional ridge (Ledoit-Wolf-style) to keep the
		// inverse well-conditioned when two metrics are highly
		// correlated — a real VPS scenario whenever bandwidth and
		// connections co-move during a traffic spike.
		var trace float64
		for a := 0; a < p; a++ {
			trace += math.Abs(cov[a][a])
		}
		ridge := trace / float64(p) * 1e-3
		if ridge < 1e-8 {
			ridge = 1e-8
		}
		for a := 0; a < p; a++ {
			cov[a][a] += ridge
		}

		var ok bool
		inv, ok = invertMatrix(cov)
		if !ok {
			return nil, nil, false
		}
	}

	return mu, inv, true
}

// squaredMahalanobis returns (x - mu)^T inv (x - mu) — the squared
// Mahalanobis distance. Guards against numerical negatives from round-off.
func squaredMahalanobis(x, mu []float64, inv [][]float64) float64 {
	p := len(x)
	diff := make([]float64, p)
	for i := 0; i < p; i++ {
		diff[i] = x[i] - mu[i]
	}
	var total float64
	for i := 0; i < p; i++ {
		var row float64
		for j := 0; j < p; j++ {
			row += inv[i][j] * diff[j]
		}
		total += diff[i] * row
	}
	if total < 0 {
		return 0
	}
	return total
}

// makeMatrix allocates a zeroed p×q matrix as a slice of slices. This is
// clearer than a flat-backed []float64 for p=q=4 and the heap cost is
// negligible — MCD refits run every 30 samples at most.
func makeMatrix(rows, cols int) [][]float64 {
	m := make([][]float64, rows)
	for i := 0; i < rows; i++ {
		m[i] = make([]float64, cols)
	}
	return m
}

// invertMatrix returns the inverse of `a` via Gauss-Jordan elimination
// with partial pivoting. Returns (nil, false) if the matrix is singular.
// a is not modified. Suitable for the p=4 matrices in this package; not
// a general-purpose linear-algebra routine.
func invertMatrix(a [][]float64) ([][]float64, bool) {
	n := len(a)
	if n == 0 {
		return nil, false
	}
	aug := makeMatrix(n, 2*n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			aug[i][j] = a[i][j]
		}
		aug[i][n+i] = 1
	}
	for col := 0; col < n; col++ {
		pivot := col
		pivotVal := math.Abs(aug[col][col])
		for row := col + 1; row < n; row++ {
			if v := math.Abs(aug[row][col]); v > pivotVal {
				pivot = row
				pivotVal = v
			}
		}
		if pivotVal < 1e-12 {
			return nil, false
		}
		if pivot != col {
			aug[col], aug[pivot] = aug[pivot], aug[col]
		}
		piv := aug[col][col]
		for j := 0; j < 2*n; j++ {
			aug[col][j] /= piv
		}
		for row := 0; row < n; row++ {
			if row == col {
				continue
			}
			factor := aug[row][col]
			if factor == 0 {
				continue
			}
			for j := 0; j < 2*n; j++ {
				aug[row][j] -= factor * aug[col][j]
			}
		}
	}
	inv := makeMatrix(n, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			inv[i][j] = aug[i][n+j]
		}
	}
	return inv, true
}
