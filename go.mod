module github.com/jiayu/wbot

go 1.22.0

require (
	github.com/jackc/pgx/v5 v5.7.2
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	golang.org/x/crypto v0.31.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

// yaml.v3's test-dep graph pulls go-internal; v1.14+ needs go 1.23+, exclude to stay go 1.22-compatible
exclude github.com/rogpeppe/go-internal v1.14.0

exclude github.com/rogpeppe/go-internal v1.14.1

exclude github.com/rogpeppe/go-internal v1.15.0
