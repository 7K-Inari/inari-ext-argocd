# Backend extension image: the plugin binary the inari-server Extension Host
# launches as a supervised sidecar (plan §5.8, §6).
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/inari-ext-argocd ./cmd/inari-ext-argocd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/inari-ext-argocd /usr/local/bin/inari-ext-argocd
USER nonroot
ENTRYPOINT ["/usr/local/bin/inari-ext-argocd"]
