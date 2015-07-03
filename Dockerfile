FROM golang:latest
# Copy the app
ADD . /app
WORKDIR /app
# Build it
ENV GOOS linux
ENV GOARCH amd64
ENV GOPATH /app/_vendor
# Setup the Go environment
RUN mkdir -p $GOPATH/src/github.com/bulletproofnetworks
RUN ln -sf $(pwd) $GOPATH/src/github.com/bulletproofnetworks/coco
RUN readlink -f /app/_vendor/src/github.com/bulletproofnetworks/coco

# Run a development copy
CMD sh -c 'go run coco_server.go coco.conf & go run noodle_server.go coco.conf'
