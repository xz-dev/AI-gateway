import socket
import ssl

context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.load_cert_chain("/cert/server.crt", "/cert/server.key")
listener = socket.socket()
listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
listener.bind(("0.0.0.0", 443))
listener.listen()

while True:
    client, _ = listener.accept()
    try:
        tls = context.wrap_socket(client, server_side=True)
        request = b""
        while b"\r\n\r\n" not in request:
            chunk = tls.recv(4096)
            if not chunk:
                break
            request += chunk
        if request:
            tls.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
        tls.close()
    except (OSError, ssl.SSLError):
        client.close()
