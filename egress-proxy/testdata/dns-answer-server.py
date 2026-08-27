import ipaddress
import socket
import struct

ANSWERS = {
    ("global.filter.example.net", 1): ["203.1.1.2"],
    ("bump.filter.example.net", 1): ["203.1.1.2"],
    ("mixed.filter.example.net", 1): ["203.1.1.2", "10.0.0.1"],
    ("private.filter.example.net", 1): ["10.0.0.1"],
    ("mapped.filter.example.net", 28): ["::ffff:10.0.0.1"],
    ("six.filter.example.net", 28): ["2002:a00:1::1"],
}
QUERY_COUNTS: dict[tuple[str, int], int] = {}


def parse_question(packet: bytes) -> tuple[str, int, int, int]:
    labels: list[str] = []
    offset = 12
    while packet[offset]:
        size = packet[offset]
        offset += 1
        labels.append(packet[offset : offset + size].decode("ascii").lower())
        offset += size
    offset += 1
    qtype, qclass = struct.unpack("!HH", packet[offset : offset + 4])
    return ".".join(labels), qtype, qclass, offset + 4


sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind(("0.0.0.0", 53))
while True:
    packet, peer = sock.recvfrom(4096)
    try:
        name, qtype, qclass, question_end = parse_question(packet)
        key = (name, qtype)
        if key == ("rebind.filter.example.net", 1) and qclass == 1:
            count = QUERY_COUNTS.get(key, 0)
            QUERY_COUNTS[key] = count + 1
            values = ["203.1.1.2"] if count == 0 else ["10.0.0.1"]
        else:
            values = ANSWERS.get(key, []) if qclass == 1 else []
        response = bytearray(
            packet[:2]
            + struct.pack("!HHHHH", 0x8180, 1, len(values), 0, 0)
            + packet[12:question_end]
        )
        for value in values:
            raw = ipaddress.ip_address(value).packed
            response.extend(struct.pack("!HHHIH", 0xC00C, qtype, 1, 30, len(raw)))
            response.extend(raw)
        sock.sendto(response, peer)
    except (IndexError, UnicodeDecodeError, ValueError, struct.error):
        continue
