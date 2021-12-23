FROM golang:latest AS build

COPY . /opt/build

RUN cd /opt/build; go build

FROM golang:latest

COPY --from=build /opt/build/ipcount /opt/ipcount

EXPOSE 8000

CMD [ "/opt/ipcount", "/config.ini", "/data/ip.db" ]


