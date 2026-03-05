#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
SNTP Client - Improved Version

Description:
    Uses the SNTP protocol (as specified in RFC 2030)
    to contact the server specified in the command line
    and report the time as returned by that server.

Improvements:
    - Class-based structure
    - argparse for command-line parsing
    - Type hints
    - Better error handling
    - Python 3 compatibility
    - Improved code organization
"""

import argparse
import socket
import struct
import sys
import time
from typing import Optional, Tuple


# Default NTP servers
DEFAULT_SERVERS = [
    "129.132.2.21",   # swisstime.ethz.ch
    "0.pool.ntp.org",
    "time.google.com",
    "time.cloudflare.com",
]

# NTP Constants
NTP_PORT = 123
NTP_TIMEOUT = 20.0
TIME1970 = 2208988800  # Seconds between 1900 and 1970


class NTPClient:
    """SNTP Client for retrieving time from NTP servers."""

    LEAP_INDICATOR = {
        0: "no warning",
        1: "last minute of current day has 61 sec",
        2: "last minute of current day has 59 sec",
        3: "alarm condition (clock not synchronized)",
    }

    MODE = {
        0: "reserved",
        1: "symmetric active",
        2: "symmetric passive",
        3: "client",
        4: "server",
        5: "broadcast",
        6: "reserved for NTP control message",
        7: "reserved for private use",
    }

    STRATUM = {
        0: "unspecified or unavailable",
        1: "primary reference (e.g. radio clock)",
        2: "2...15: secondary reference (via NTP or SNTP)",
        16: "16...255: reserved",
    }

    def __init__(self, server: str, ntp_version: int = 4, timeout: float = NTP_TIMEOUT):
        """
        Initialize the NTP client.

        Args:
            server: NTP server hostname or IP address
            ntp_version: NTP version (3 or 4)
            timeout: Socket timeout in seconds
        """
        self.server = server
        self.ntp_version = ntp_version
        self.timeout = timeout

    def _create_ntp_request(self) -> bytes:
        """Create an NTP request packet."""
        if self.ntp_version == 3:
            # SNTP Message version 3
            return b"\x1b" + 47 * b"\0"
        elif self.ntp_version == 4:
            # SNTP Message version 4
            return b"\x23" + 47 * b"\0"
        else:
            raise ValueError(f"Unsupported NTP version: {self.ntp_version}")

    def _decode_first_byte(self, byte1: int) -> Tuple[int, int, int]:
        """
        Decode the first byte of the NTP response.

        Returns:
            Tuple of (leap_indicator, version_number, mode)
        """
        li = (byte1 & 0xC0) >> 6
        vn = (byte1 & 0x38) >> 3
        mode = byte1 & 0x07
        return li, vn, mode

    def _normalize_stratum(self, stratum: int) -> int:
        """Normalize the stratum value for display."""
        if stratum == 1:
            return 1
        elif 2 <= stratum <= 15:
            return 2
        elif stratum > 15:
            return 16
        return 0

    def _interpret_reference_id(self, reference_id: bytes, stratum: int, vn: int) -> str:
        """Interpret the reference identification field."""
        if stratum == 1:
            # Primary reference - decode as 4-character string
            try:
                return reference_id.decode('ascii').strip('\x00')
            except:
                return str(reference_id)
        elif stratum == 2:
            if vn == 3:
                return "IPv4 address: {}.{}.{}.{}".format(*struct.unpack("4B", reference_id))
            elif vn == 4:
                return "Ref ID: {:02X}{:02X}{:02X}{:02X}".format(*reference_id)
        return str(reference_id)

    def _convert_ntp_time(self, integer: int, fraction: int) -> float:
        """Convert NTP timestamp to Unix epoch."""
        return integer + (fraction / 2**32) - TIME1970

    def fetch_time(self) -> Optional[dict]:
        """
        Fetch time from the NTP server.

        Returns:
            Dictionary containing time information, or None if failed
        """
        message = self._create_ntp_request()
        originate_time = time.time()

        try:
            socket.setdefaulttimeout(self.timeout)
            client = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            client.sendto(message, (self.server, NTP_PORT))

            data, address = client.recvfrom(1024)
            destination_time = time.time()
            client.close()

            return self._parse_response(
                data, address, originate_time, destination_time
            )

        except socket.timeout:
            print(f"Error: Connection to {self.server} timed out", file=sys.stderr)
            return None
        except socket.gaierror as e:
            print(f"Error: Failed to resolve {self.server}: {e}", file=sys.stderr)
            return None
        except OSError as e:
            print(f"Error: Network error: {e}", file=sys.stderr)
            return None

    def _parse_response(
        self, data: bytes, address: Tuple[str, int],
        originate_time: float, destination_time: float
    ) -> dict:
        """Parse the NTP response packet."""
        # Unpack the response (format: !2B2b2i4s8I)
        # ! = network byte order, B = unsigned char, b = signed char, i = int32, s = string, I = unsigned int32
        unpacked = struct.unpack("!2B2b2i4s8I", data)

        (
            byte1, stratum, poll, precision,
            root_delay, root_dispersion,
            reference_id,
            reference_time, reference_time_fb,
            originate_time2, originate_time2_fb,
            receive_time, receive_time_fb,
            transmit_time, transmit_time_fb,
        ) = unpacked

        # Decode first byte
        li, vn, mode = self._decode_first_byte(byte1)

        # Normalize stratum
        stratum = self._normalize_stratum(stratum)

        # Interpret reference ID
        ref_id = self._interpret_reference_id(reference_id, stratum, vn)

        # Convert times to Unix epoch
        receive_time = self._convert_ntp_time(receive_time, receive_time_fb)
        reference_time = self._convert_ntp_time(reference_time, reference_time_fb)
        originate_time2 = self._convert_ntp_time(originate_time2, originate_time2_fb)
        transmit_time = self._convert_ntp_time(transmit_time, transmit_time_fb)

        # Calculate clock offset and roundtrip delay (RFC 2030)
        clock_offset = (
            (receive_time - originate_time) + (transmit_time - destination_time)
        ) / 2.0
        roundtrip_delay = (destination_time - originate_time) - (receive_time - transmit_time)

        return {
            "server": self.server,
            "address": address,
            "leap_indicator": li,
            "version": vn,
            "mode": mode,
            "stratum": stratum,
            "poll": poll,
            "precision": precision,
            "root_delay": root_delay,
            "root_dispersion": root_dispersion,
            "reference_id": ref_id,
            "reference_time": reference_time,
            "originate_time": originate_time,
            "receive_time": receive_time,
            "transmit_time": transmit_time,
            "destination_time": destination_time,
            "clock_offset": clock_offset,
            "roundtrip_delay": roundtrip_delay,
        }

    def print_result(self, result: dict) -> None:
        """Print the NTP time result in a formatted way."""
        print()
        print(f"Response received from : {result['server']}")
        print(f"IP address             : {result['address'][0]}")
        print(f"Port                   : {result['address'][1]}")
        print()
        print("Header")
        print("-" * 50)
        print(f"Byte1                  : 0x{result['leap_indicator'] << 6 | result['version'] << 3 | result['mode']:02X}")
        print(f"  Leap Indicator (LI)  : {result['leap_indicator']} [{self.LEAP_INDICATOR[result['leap_indicator']]}]")
        print(f"  Version number (VN)  : {result['version']} [NTP/SNTP version number]")
        print(f"  Mode                 : {result['mode']} [{self.MODE[result['mode']]}]")
        print(f"Stratum                : {result['stratum']} [{self.STRATUM[result['stratum']]}]")
        print(f"Poll interval          : {result['poll']}")
        print(f"Clock Precision        : 2**{result['precision']} = {2 ** result['precision']:.5E}")
        print(f"Root Delay             : 0x{result['root_delay']:08X} = {result['root_delay'] / 2**16:.5f}")
        print(f"Root Dispersion        : 0x{result['root_dispersion']:08X} = {result['root_dispersion'] / 2**16:.5f}")
        print(f"Reference Identifier   : {result['reference_id']}")
        print()
        print("Interpreted results (Unix epoch):")
        print("-" * 50)
        print(f"Reference Timestamp    : {result['reference_time']:.5f} [last sync of server clock]")
        print(f"Originate Timestamp    : {result['originate_time']:.5f} [request sent by client]")
        print(f"Receive   Timestamp    : {result['receive_time']:.5f} [request received by server]")
        print(f"Transmit  Timestamp    : {result['transmit_time']:.5f} [reply sent by server]")
        print(f"Destination Timestamp  : {result['destination_time']:.5f} [reply received by client]")
        print("-" * 50)
        print()
        print(f"Net Time UTC           : {time.ctime(result['receive_time'])} + {result['receive_time'] % 1 * 1000:.3f} ms")
        print(f"Clock Offset           : {result['clock_offset']:.5f} seconds")
        print(f"Roundtrip Delay        : {result['roundtrip_delay']:.5f} seconds")


def parse_arguments() -> argparse.Namespace:
    """Parse command-line arguments."""
    parser = argparse.ArgumentParser(
        description="SNTP client - Get time from NTP servers",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
    %(prog)s                          # Use default server
    %(prog)s time.google.com          # Use Google NTP server
    %(prog)s -v 3 0.pool.ntp.org      # Use NTP version 3
    %(prog)s -t 10 ntp.metas.ch       # 10 second timeout
        """
    )

    parser.add_argument(
        "server",
        nargs="?",
        default=DEFAULT_SERVERS[0],
        help=f"NTP server hostname or IP (default: {DEFAULT_SERVERS[0]})"
    )

    parser.add_argument(
        "-v", "--version",
        type=int,
        choices=[3, 4],
        default=4,
        help="NTP version (3 or 4, default: 4)"
    )

    parser.add_argument(
        "-t", "--timeout",
        type=float,
        default=NTP_TIMEOUT,
        help=f"Socket timeout in seconds (default: {NTP_TIMEOUT})"
    )

    parser.add_argument(
        "-l", "--list-servers",
        action="store_true",
        help="List available default NTP servers"
    )

    return parser.parse_args()


def main() -> int:
    """Main entry point."""
    args = parse_arguments()

    if args.list_servers:
        print("Available default NTP servers:")
        for server in DEFAULT_SERVERS:
            print(f"  - {server}")
        return 0

    # Create client and fetch time
    client = NTPClient(
        server=args.server,
        ntp_version=args.version,
        timeout=args.timeout
    )

    result = client.fetch_time()

    if result:
        client.print_result(result)
        return 0
    else:
        return 1


if __name__ == "__main__":
    main()
