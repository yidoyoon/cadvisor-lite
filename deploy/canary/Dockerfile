FROM golang:1.20
MAINTAINER dashpole@google.com

RUN apt-get update && apt-get install -y git dmsetup && apt-get clean
RUN git clone https://github.com/yidoyoon/cadvisor-lite.git /go/src/github.com/yidoyoon/cadvisor-lite
RUN cd /go/src/github.com/yidoyoon/cadvisor-lite && make

ENTRYPOINT ["/go/src/github.com/yidoyoon/cadvisor-lite/cadvisor"]

