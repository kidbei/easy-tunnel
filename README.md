# easy-tunnel

## 编译

`cd client && go build`

`cd server && go build`

## 帮助

`./server -h`

-host string
    服务器通信ip (default "0.0.0.0")

-port int
    通信端口 (default 9960)

*****************************************

`./client -h`

-bh string
    开启映射后绑定ip (default "0.0.0.0")

  -bp int
    远程开启的映射端口,必填 (default -1)

  -fh string
    转发目标ip (default "127.0.0.1")

  -fp int
    转发目标端口,必填 (default -1)

  -h string
    远程服务器通信ip (default "127.0.0.1")

  -p int
    远程服务器通信端口 (default 9960)