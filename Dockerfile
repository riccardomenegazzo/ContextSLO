FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /contextslo ./cmd/contextslo

FROM scratch
COPY --from=build /contextslo /contextslo
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/contextslo"]
CMD ["serve", "--listen", ":8080", "--data", "/data/state.json"]
