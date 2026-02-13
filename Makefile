.PHONY: randomx build test test-cgo clean

randomx:
	cd third_party/RandomX && mkdir -p build && cd build && cmake .. -DCMAKE_BUILD_TYPE=Release && make -j$$(sysctl -n hw.ncpu)

build: randomx
	CGO_ENABLED=1 go build ./cmd/chrd

test:
	go test -v ./pkg/core/types/... ./pkg/core/consensus/... ./pkg/core/blockchain/...

test-cgo: randomx
	CGO_ENABLED=1 go test -tags randomx -v ./pkg/core/consensus/randomx/...

clean:
	rm -rf third_party/RandomX/build
