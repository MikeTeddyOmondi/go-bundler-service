default:
    just --list

build-image:
    docker build -t ranckosolutionsinc/bundler-service:v1-go . 

run-container:
    docker run -dp 8080:8080 \
    --name bundler-svc-go \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -e _EXPERIMENTAL_DAGGER_RUNNER_HOST=docker-container://dagger-engine-v0.14.0 \
    ranckosolutionsinc/bundler-service:v1-go

log:
    docker logs -f bundler-svc-go

stop-container:
    docker stop bundler-svc-go

rm-container:
    docker stop bundler-svc-go
    docker rm bundler-svc-go

