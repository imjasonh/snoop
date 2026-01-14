# Quick Start: KinD Testing

## One-Time Setup

```bash
# Navigate to test directory
cd test/kind

# Install prerequisites (macOS)
go install sigs.k8s.io/kind@latest
brew install kubectl jq

# Ensure Docker Desktop is running
docker ps
```

## Run Tests

```bash
# Complete test cycle (setup, test, teardown)
./setup.sh && ./run-tests.sh && ./teardown.sh

# Or run steps individually:

# Step 1: Setup (5-10 minutes)
./setup.sh

# Step 2: Run tests (2-3 minutes per test)
./run-tests.sh

# Step 3: Teardown
./teardown.sh
```

## What Happens

### setup.sh
1. Creates KinD cluster named `snoop-test`
2. Builds snoop Docker image (with eBPF code generation)
3. Loads image into cluster
4. Applies RBAC resources
5. Takes ~5-10 minutes

### run-tests.sh
1. Deploys alpine test → validates report
2. Deploys busybox test → validates report
3. Each test takes ~2-3 minutes
4. Saves results to `results/`

### teardown.sh
1. Deletes KinD cluster
2. Cleans up temp files
3. Takes ~10 seconds

## Expected Output

### Success

```
================================================
Test Summary
================================================

Passed: 2
Failed: 0

✅ All tests passed!

Results saved to: /path/to/results
```

### Failure

```
❌ FAILED: Could not retrieve report

Snoop logs:
[logs showing error]

...

================================================
Test Summary
================================================

Passed: 1
Failed: 1

❌ Some tests failed:
  - alpine-basic: report not found

Results saved to: /path/to/results
Check logs for details
```

## Inspect Results

```bash
# View all result files
ls -lh results/

# View a report
cat results/alpine-basic-report.json | jq .

# View snoop logs
cat results/alpine-basic-snoop.log

# View validation output
cat results/alpine-basic-validation.log
```

## Manual Testing

If you want to test manually:

```bash
# Setup cluster
./setup.sh

# Deploy manually
kubectl apply -f manifests/alpine-test.yaml

# Wait for pod
kubectl wait --for=condition=Ready pod -l app=alpine-test -n snoop-test --timeout=90s

# Check logs
kubectl -n snoop-test logs -l app=alpine-test -c snoop -f

# Wait ~35 seconds, then retrieve report
POD=$(kubectl -n snoop-test get pod -l app=alpine-test -o jsonpath='{.items[0].metadata.name}')
kubectl -n snoop-test cp $POD:/data/snoop-report.json ./my-report.json -c app

# Validate
cd validate && go build
./validate ../my-report.json

# Cleanup
kubectl delete -f manifests/alpine-test.yaml
```

## Troubleshooting

### "kind not found"
```bash
go install sigs.k8s.io/kind@latest
```

### "Docker not running"
```bash
# Start Docker Desktop, then:
docker ps
```

### "Cluster already exists"
```bash
./teardown.sh
./setup.sh
```

### "Pod not ready"
```bash
# Check pod status
kubectl -n snoop-test get pods

# Check events
kubectl -n snoop-test describe pod <pod-name>

# Check logs
kubectl -n snoop-test logs <pod-name> -c snoop
kubectl -n snoop-test logs <pod-name> -c app
```

### "Report not found"
```bash
# Check if report file exists
kubectl -n snoop-test exec <pod-name> -c app -- ls -la /data/

# Check snoop logs for errors
kubectl -n snoop-test logs <pod-name> -c snoop --tail=100
```

## Next Steps

After tests pass locally:
1. Review `KIND_TESTING_PLAN.md` for additional test scenarios
2. Test on real cluster (GKE/EKS)
3. Add more test cases as needed
4. Report any issues found

## Getting Help

- Check `README.md` in this directory for detailed docs
- Check `KIND_TESTING_PLAN.md` for testing strategy
- Check pod logs and events for runtime issues
- Check `results/` directory for test artifacts
