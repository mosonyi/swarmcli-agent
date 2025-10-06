IMAGE_ORG ?= mosonyi
AGENT_IMAGE := $(IMAGE_ORG)/swarmcli-agent:local
PROXY_IMAGE := $(IMAGE_ORG)/swarmcli-proxy:local

BIN_DIR := bin

.PHONY: build push cli-build cli-push all-build all-push test demo clean local-build

# --- Docker image builds ---
build:
	docker build -f Dockerfile.agent -t $(AGENT_IMAGE) .
	docker build -f Dockerfile.proxy -t $(PROXY_IMAGE) .

push:
	docker push $(AGENT_IMAGE)
	docker push $(PROXY_IMAGE)

all-build: build
all-push: push

# --- Local Go builds ---
local-build: $(BIN_DIR)
	go build -o $(BIN_DIR)/agent ./cmd/agent
	go build -o $(BIN_DIR)/proxy ./cmd/proxy
	go build -o $(BIN_DIR)/cli ./cmd/cli
	go build -o $(BIN_DIR)/test ./cmd/test

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

# --- Testing & Demo ---
test:
	./scripts/test.sh $$TASK_ID

demo:
	docker service create --name demo-nginx --detach=true --replicas=1 --publish 8088:80 nginx:alpine
	@echo "Demo service 'demo-nginx' created. Use 'docker service ps demo-nginx' to find a TASK_ID."
	@echo "Then run: make test TASK_ID=<TASK_ID>"

clean:
	docker service rm demo-nginx || true
	docker stack rm swarmctl || true
	rm -rf $(BIN_DIR)
	@echo "Cleaned up demo service, swarmctl stack, and local binaries."
