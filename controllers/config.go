// config.go
package controllers

type ServerCfg struct {
	Id   int
	Host string
	User string
	Pass string
	Cert string
}

var serverPoolCfg = []byte(`
[
    {
        "id": 1,
        "host": "127.0.0.1:18554",
        "user": "USER",
        "pass": "PASSWORD",
        "cert": "\/home\/mydesktop\/.dcrwallet\/rpc.cert"
    },
    {
        "id": 2,
        "host": "127.0.0.2:18558",
        "user": "USER",
        "pass": "PASSWORD",
        "cert": "\/home\/mydesktop\/.dcrwallet2\/rpc.cert"
    }
]
`)
