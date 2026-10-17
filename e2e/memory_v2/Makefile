.PHONY: memory-e2e-up memory-e2e-down memory-e2e-seed memory-e2e-mcp memory-e2e-ui memory-e2e-test
memory-e2e-up:
	./e2e/memory_v2/stack.sh up
memory-e2e-down:
	./e2e/memory_v2/stack.sh down
memory-e2e-seed:
	go run ./e2e/memory_v2/seed
memory-e2e-mcp:
	test -s e2e/memory_v2/seed-output.json
	go test -tags memory_e2e -count=1 -v ./e2e/memory_v2/mcpclient/
memory-e2e-ui:
	npm --prefix frontend run test:e2e:memory
memory-e2e-test: memory-e2e-mcp memory-e2e-ui
