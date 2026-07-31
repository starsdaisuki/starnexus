# Publication review status

This directory is intentionally fail-closed. Each approved image and data
artifact is bound to its exact repository path and SHA-256, so any content
change requires a new review.

The repository contains two source classes that must not be conflated:

- project-generated figures and tabular outputs derived from anonymized,
  real fault-injection experiments;
- vendored Leaflet image assets from the upstream public distribution.

The first class is not synthetic. Its manifests classify it as anonymized
owner-derived measurements only after infrastructure identifiers and
re-identification risk were reviewed. The Leaflet assets were visually and
metadata-reviewed as public third-party distribution files.
