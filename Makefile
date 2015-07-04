build-path := /tmp/coco

image:
	docker build -t coco .

run: image
	docker run -p 25826:25826/udp -p 9090:9090 -p 9080:9080 -ti coco

test: image
	docker run -ti coco make go-test

go-test:
	go test -v coco/coco_test.go
	go test -v noodle/noodle_test.go

release: image
	docker run -ti -v $(shell pwd)/release:/app/release coco make go-release
	cp release/coco.tar.gz .
	rm -rf release
	echo "Release is at ./coco.tar.gz"

go-release: go-test
	mkdir -p $(build-path)
	go build -o $(build-path)/coco coco_server.go
	go build -o $(build-path)/noodle noodle_server.go
	go build -o $(build-path)/check_anomalous_coco_errors anomalous_coco_errors.go
	go build -o $(build-path)/check_anomalous_coco_send anomalous_coco_send.go
	cp -av etc $(build-path)
	cp coco.sample.conf $(build-path)
	cd /tmp && tar czvf /app/release/coco.tar.gz coco
