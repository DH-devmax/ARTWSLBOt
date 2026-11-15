# Local provenance smoke fixture; never a release or production image.
FROM scratch
LABEL ci.fixture="image-provenance"
COPY marker.txt /ci-fixture-marker
