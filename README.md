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

## 示例

比如我们有公网服务器10.11.11.14,现在我本地开启了一个8080的http服务，希望访问10.11.11.14:9999能访问到本地的8080服务。

1. 在10.11.11.14运行服务端:

  `./server -host 0.0.0.0 -port 9960`
  
2. 在本地运行客户端

  `./client -h 10.11.11.14 -p 9960 -bh 0.0.0.0 -bp 9999 -fh 127.0.0.1 -fp 8080`