# Backend extension image: the plugin binary the inari-server Extension Host
# launches as a supervised sidecar (plan §5.8, §6).
FROM golang:1.27@sha256:e432b43af23a9328d56a7c499be0476810aa344acbcf65fc7c455d4ff5a40602 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/inari-ext-argocd ./cmd/inari-ext-argocd

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/inari-ext-argocd /usr/local/bin/inari-ext-argocd
USER nonroot
ENTRYPOINT ["/usr/local/bin/inari-ext-argocd"]
