# Build WASM
fastly compute build

# Deploy
fastly compute publish

# Test on Edge
curl -X POST "https://YOUR-SERVICE.map.fastly.net/iperf/client/run" \
  -H "Content-Type: application/json" \
  -d '{"server_host":"iperf.he.net","duration":3}'

