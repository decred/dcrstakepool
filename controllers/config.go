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
        "host": "127.0.0.1:19110",
        "user": "USER",
        "pass": "PASSWORD",
        "cert": "\/home\/mydesktop\/.dcrstakepool\/wallet1.cert"
    },
    {
        "id": 2,
        "host": "127.0.0.2:19110",
        "user": "USER",
        "pass": "PASSWORD",
        "cert": "\/home\/mydesktop\/.dcrstakepool\/wallet2.cert"
    }
]
`)
