#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Unit tests for the NTP Client (ntpClient.py).
"""

import struct
import sys
import unittest

sys.path.insert(0, "src")
from ntpClient import NTPAddress, NTPClient, NTPResult, TIME1970


class TestDecodeFirstByte(unittest.TestCase):
    """Tests for NTPClient._decode_first_byte(byte1)."""

    def setUp(self):
        self.client = NTPClient(server="test.server", ntp_version=4)

    def test_all_zeros(self):
        li, vn, mode = self.client._decode_first_byte(0x00)
        self.assertEqual(li, 0)
        self.assertEqual(vn, 0)
        self.assertEqual(mode, 0)

    def test_version4_server_mode(self):
        # 0x24 = 0b00_100_100 => LI=0, VN=4, Mode=4
        li, vn, mode = self.client._decode_first_byte(0x24)
        self.assertEqual(li, 0)
        self.assertEqual(vn, 4)
        self.assertEqual(mode, 4)

    def test_version3_client_mode(self):
        # 0x1b = 0b00_011_011 => LI=0, VN=3, Mode=3
        li, vn, mode = self.client._decode_first_byte(0x1B)
        self.assertEqual(li, 0)
        self.assertEqual(vn, 3)
        self.assertEqual(mode, 3)

    def test_version4_client_mode(self):
        # 0x23 = 0b00_100_011 => LI=0, VN=4, Mode=3
        li, vn, mode = self.client._decode_first_byte(0x23)
        self.assertEqual(li, 0)
        self.assertEqual(vn, 4)
        self.assertEqual(mode, 3)

    def test_leap_indicator_3(self):
        # 0xE4 = 0b11_100_100 => LI=3, VN=4, Mode=4
        li, vn, mode = self.client._decode_first_byte(0xE4)
        self.assertEqual(li, 3)
        self.assertEqual(vn, 4)
        self.assertEqual(mode, 4)

    def test_leap_indicator_1(self):
        # 0x64 = 0b01_100_100 => LI=1, VN=4, Mode=4
        li, vn, mode = self.client._decode_first_byte(0x64)
        self.assertEqual(li, 1)
        self.assertEqual(vn, 4)
        self.assertEqual(mode, 4)

    def test_leap_indicator_2(self):
        # 0xA4 = 0b10_100_100 => LI=2, VN=4, Mode=4
        li, vn, mode = self.client._decode_first_byte(0xA4)
        self.assertEqual(li, 2)
        self.assertEqual(vn, 4)
        self.assertEqual(mode, 4)

    def test_mode_broadcast(self):
        # 0x25 = 0b00_100_101 => LI=0, VN=4, Mode=5
        li, vn, mode = self.client._decode_first_byte(0x25)
        self.assertEqual(li, 0)
        self.assertEqual(vn, 4)
        self.assertEqual(mode, 5)

    def test_all_bits_set(self):
        # 0xFF = 0b11_111_111 => LI=3, VN=7, Mode=7
        li, vn, mode = self.client._decode_first_byte(0xFF)
        self.assertEqual(li, 3)
        self.assertEqual(vn, 7)
        self.assertEqual(mode, 7)


class TestNormalizeStratum(unittest.TestCase):
    """Tests for NTPClient._normalize_stratum(stratum)."""

    def setUp(self):
        self.client = NTPClient(server="test.server", ntp_version=4)

    def test_stratum_0(self):
        self.assertEqual(self.client._normalize_stratum(0), 0)

    def test_stratum_1(self):
        self.assertEqual(self.client._normalize_stratum(1), 1)

    def test_stratum_2(self):
        self.assertEqual(self.client._normalize_stratum(2), 2)

    def test_stratum_10(self):
        self.assertEqual(self.client._normalize_stratum(10), 2)

    def test_stratum_15(self):
        self.assertEqual(self.client._normalize_stratum(15), 2)

    def test_stratum_16(self):
        self.assertEqual(self.client._normalize_stratum(16), 16)

    def test_stratum_255(self):
        self.assertEqual(self.client._normalize_stratum(255), 16)


class TestInterpretReferenceId(unittest.TestCase):
    """Tests for NTPClient._interpret_reference_id(reference_id_bytes, stratum, vn)."""

    def setUp(self):
        self.client = NTPClient(server="test.server", ntp_version=4)

    def test_stratum1_ascii_string(self):
        ref_id = b"GOOG"
        result = self.client._interpret_reference_id(ref_id, stratum=1, vn=4)
        self.assertEqual(result, "GOOG")

    def test_stratum1_ascii_with_null_padding(self):
        ref_id = b"GPS\x00"
        result = self.client._interpret_reference_id(ref_id, stratum=1, vn=4)
        self.assertEqual(result, "GPS")

    def test_stratum2_vn3_ipv4_address(self):
        ref_id = struct.pack("4B", 192, 168, 1, 1)
        result = self.client._interpret_reference_id(ref_id, stratum=2, vn=3)
        self.assertEqual(result, "IPv4 address: 192.168.1.1")

    def test_stratum2_vn4_hex_ref_id(self):
        ref_id = bytes([0xAB, 0xCD, 0xEF, 0x01])
        result = self.client._interpret_reference_id(ref_id, stratum=2, vn=4)
        self.assertEqual(result, "Ref ID: ABCDEF01")

    def test_stratum2_vn3_loopback(self):
        ref_id = struct.pack("4B", 127, 0, 0, 1)
        result = self.client._interpret_reference_id(ref_id, stratum=2, vn=3)
        self.assertEqual(result, "IPv4 address: 127.0.0.1")

    def test_stratum2_vn4_all_zeros(self):
        ref_id = bytes([0x00, 0x00, 0x00, 0x00])
        result = self.client._interpret_reference_id(ref_id, stratum=2, vn=4)
        self.assertEqual(result, "Ref ID: 00000000")


class TestConvertNtpTime(unittest.TestCase):
    """Tests for NTPClient._convert_ntp_time(integer, fraction)."""

    def setUp(self):
        self.client = NTPClient(server="test.server", ntp_version=4)

    def test_epoch_1970(self):
        # NTP time at Unix epoch = TIME1970 seconds, 0 fraction
        result = self.client._convert_ntp_time(TIME1970, 0)
        self.assertAlmostEqual(result, 0.0, places=5)

    def test_known_timestamp(self):
        # 1 second after Unix epoch
        result = self.client._convert_ntp_time(TIME1970 + 1, 0)
        self.assertAlmostEqual(result, 1.0, places=5)

    def test_fraction_half(self):
        # Half-second fraction: 2^31
        result = self.client._convert_ntp_time(TIME1970, 2**31)
        self.assertAlmostEqual(result, 0.5, places=5)

    def test_fraction_quarter(self):
        # Quarter-second fraction: 2^30
        result = self.client._convert_ntp_time(TIME1970, 2**30)
        self.assertAlmostEqual(result, 0.25, places=5)

    def test_large_timestamp(self):
        # A specific Unix timestamp: 1_700_000_000
        ntp_int = 1_700_000_000 + TIME1970
        result = self.client._convert_ntp_time(ntp_int, 0)
        self.assertAlmostEqual(result, 1_700_000_000.0, places=5)

    def test_zero_ntp_time(self):
        # NTP time 0 should be -TIME1970 in Unix epoch
        result = self.client._convert_ntp_time(0, 0)
        self.assertAlmostEqual(result, -TIME1970, places=5)


class TestCreateNtpRequest(unittest.TestCase):
    """Tests for NTPClient._create_ntp_request()."""

    def test_version3_packet(self):
        client = NTPClient(server="test.server", ntp_version=3)
        packet = client._create_ntp_request()
        self.assertEqual(len(packet), 48)
        self.assertEqual(packet[0], 0x1B)
        self.assertEqual(packet[1:], b"\x00" * 47)

    def test_version4_packet(self):
        client = NTPClient(server="test.server", ntp_version=4)
        packet = client._create_ntp_request()
        self.assertEqual(len(packet), 48)
        self.assertEqual(packet[0], 0x23)
        self.assertEqual(packet[1:], b"\x00" * 47)

    def test_unsupported_version(self):
        client = NTPClient(server="test.server", ntp_version=5)
        with self.assertRaises(ValueError):
            client._create_ntp_request()

    def test_version3_first_byte_decode(self):
        client = NTPClient(server="test.server", ntp_version=3)
        packet = client._create_ntp_request()
        li, vn, mode = client._decode_first_byte(packet[0])
        self.assertEqual(li, 0)
        self.assertEqual(vn, 3)
        self.assertEqual(mode, 3)

    def test_version4_first_byte_decode(self):
        client = NTPClient(server="test.server", ntp_version=4)
        packet = client._create_ntp_request()
        li, vn, mode = client._decode_first_byte(packet[0])
        self.assertEqual(li, 0)
        self.assertEqual(vn, 4)
        self.assertEqual(mode, 3)


class TestParseResponse(unittest.TestCase):
    """Tests for NTPClient._parse_response(data, address_tuple, originate_time, destination_time)."""

    def setUp(self):
        self.client = NTPClient(server="test.server", ntp_version=4)

    def _build_ntp_packet(
        self,
        byte1=0x24,
        stratum=1,
        poll=5,
        precision=-18,
        root_delay=0,
        root_dispersion=0,
        reference_id=b"GOOG",
        ref_sec=None,
        ref_frac=0,
        orig_sec=None,
        orig_frac=0,
        recv_sec=None,
        recv_frac=0,
        xmit_sec=None,
        xmit_frac=0,
    ):
        """Build a synthetic 48-byte NTP response packet."""
        base_ntp = TIME1970 + 1_700_000_000
        if ref_sec is None:
            ref_sec = base_ntp
        if orig_sec is None:
            orig_sec = base_ntp + 1
        if recv_sec is None:
            recv_sec = base_ntp + 2
        if xmit_sec is None:
            xmit_sec = base_ntp + 3

        return struct.pack(
            "!2B2b2i4s8I",
            byte1,
            stratum,
            poll,
            precision,
            root_delay,
            root_dispersion,
            reference_id,
            ref_sec, ref_frac,
            orig_sec, orig_frac,
            recv_sec, recv_frac,
            xmit_sec, xmit_frac,
        )

    def test_packet_length(self):
        packet = self._build_ntp_packet()
        self.assertEqual(len(packet), 48)

    def test_basic_parse(self):
        packet = self._build_ntp_packet()
        address = ("1.2.3.4", 123)
        originate_time = 1_700_000_000.0
        destination_time = 1_700_000_004.0

        result = self.client._parse_response(packet, address, originate_time, destination_time)

        self.assertIsInstance(result, NTPResult)
        self.assertEqual(result.server, "test.server")
        self.assertIsInstance(result.address, NTPAddress)
        self.assertEqual(result.address.ip, "1.2.3.4")
        self.assertEqual(result.address.port, 123)

    def test_header_fields(self):
        # 0x24 = LI=0, VN=4, Mode=4
        packet = self._build_ntp_packet(byte1=0x24, stratum=1, poll=5, precision=-18)
        address = ("10.0.0.1", 123)

        result = self.client._parse_response(packet, address, 0.0, 0.0)

        self.assertEqual(result.leap_indicator, 0)
        self.assertEqual(result.version, 4)
        self.assertEqual(result.mode, 4)
        # stratum 1 normalizes to 1
        self.assertEqual(result.stratum, 1)
        self.assertEqual(result.poll, 5)
        self.assertEqual(result.precision, -18)

    def test_root_delay_and_dispersion(self):
        packet = self._build_ntp_packet(root_delay=100, root_dispersion=200)
        address = ("10.0.0.1", 123)

        result = self.client._parse_response(packet, address, 0.0, 0.0)

        self.assertEqual(result.root_delay, 100)
        self.assertEqual(result.root_dispersion, 200)

    def test_reference_id_stratum1(self):
        packet = self._build_ntp_packet(byte1=0x24, stratum=1, reference_id=b"GOOG")
        address = ("10.0.0.1", 123)

        result = self.client._parse_response(packet, address, 0.0, 0.0)

        self.assertEqual(result.reference_id, "GOOG")

    def test_reference_id_stratum2_vn3(self):
        # Build with VN=3, Mode=4 => byte1 = 0b00_011_100 = 0x1C
        ref_id = struct.pack("4B", 10, 20, 30, 40)
        packet = self._build_ntp_packet(byte1=0x1C, stratum=2, reference_id=ref_id)
        address = ("10.0.0.1", 123)

        result = self.client._parse_response(packet, address, 0.0, 0.0)

        self.assertEqual(result.reference_id, "IPv4 address: 10.20.30.40")

    def test_reference_id_stratum2_vn4(self):
        ref_id = bytes([0xAB, 0xCD, 0xEF, 0x01])
        packet = self._build_ntp_packet(byte1=0x24, stratum=2, reference_id=ref_id)
        address = ("10.0.0.1", 123)

        result = self.client._parse_response(packet, address, 0.0, 0.0)

        self.assertEqual(result.reference_id, "Ref ID: ABCDEF01")

    def test_timestamps_conversion(self):
        base_ntp = TIME1970 + 1_700_000_000
        packet = self._build_ntp_packet(
            ref_sec=base_ntp,
            ref_frac=0,
            orig_sec=base_ntp + 1,
            orig_frac=0,
            recv_sec=base_ntp + 2,
            recv_frac=0,
            xmit_sec=base_ntp + 3,
            xmit_frac=0,
        )
        address = ("10.0.0.1", 123)
        originate_time = 1_700_000_000.0
        destination_time = 1_700_000_005.0

        result = self.client._parse_response(packet, address, originate_time, destination_time)

        self.assertAlmostEqual(result.reference_time, 1_700_000_000.0, places=5)
        self.assertAlmostEqual(result.receive_time, 1_700_000_002.0, places=5)
        self.assertAlmostEqual(result.transmit_time, 1_700_000_003.0, places=5)
        # originate_time is the one passed in, not from the packet
        self.assertAlmostEqual(result.originate_time, originate_time, places=5)
        self.assertAlmostEqual(result.destination_time, destination_time, places=5)

    def test_timestamps_with_fractions(self):
        base_ntp = TIME1970 + 1_700_000_000
        half_frac = 2**31  # 0.5 seconds

        packet = self._build_ntp_packet(
            ref_sec=base_ntp,
            ref_frac=half_frac,
            orig_sec=base_ntp + 1,
            orig_frac=0,
            recv_sec=base_ntp + 2,
            recv_frac=half_frac,
            xmit_sec=base_ntp + 3,
            xmit_frac=0,
        )
        address = ("10.0.0.1", 123)

        result = self.client._parse_response(packet, address, 0.0, 0.0)

        self.assertAlmostEqual(result.reference_time, 1_700_000_000.5, places=5)
        self.assertAlmostEqual(result.receive_time, 1_700_000_002.5, places=5)
        self.assertAlmostEqual(result.transmit_time, 1_700_000_003.0, places=5)

    def test_clock_offset_and_roundtrip_delay(self):
        """
        Clock offset = ((receive - originate) + (transmit - destination)) / 2
        Roundtrip delay = (destination - originate) - (receive - transmit)

        With:
          originate_time (T1) = 1000.0
          receive_time   (T2) = 1001.0
          transmit_time  (T3) = 1002.0
          destination    (T4) = 1005.0

        offset = ((1001 - 1000) + (1002 - 1005)) / 2 = (1 + (-3)) / 2 = -1.0
        delay  = (1005 - 1000) - (1001 - 1002) = 5 - (-1) = 6.0
        """
        base_ntp = TIME1970 + 1000
        packet = self._build_ntp_packet(
            ref_sec=base_ntp,
            ref_frac=0,
            orig_sec=base_ntp,
            orig_frac=0,
            recv_sec=base_ntp + 1,
            recv_frac=0,
            xmit_sec=base_ntp + 2,
            xmit_frac=0,
        )
        address = ("10.0.0.1", 123)
        originate_time = 1000.0
        destination_time = 1005.0

        result = self.client._parse_response(packet, address, originate_time, destination_time)

        self.assertAlmostEqual(result.receive_time, 1001.0, places=5)
        self.assertAlmostEqual(result.transmit_time, 1002.0, places=5)

        expected_offset = ((1001.0 - 1000.0) + (1002.0 - 1005.0)) / 2.0
        expected_delay = (1005.0 - 1000.0) - (1001.0 - 1002.0)
        self.assertAlmostEqual(result.clock_offset, expected_offset, places=5)
        self.assertAlmostEqual(result.roundtrip_delay, expected_delay, places=5)

    def test_zero_offset_scenario(self):
        """When server and client clocks are perfectly synchronized."""
        base_ntp = TIME1970 + 5000
        packet = self._build_ntp_packet(
            ref_sec=base_ntp,
            ref_frac=0,
            orig_sec=base_ntp,
            orig_frac=0,
            recv_sec=base_ntp + 1,
            recv_frac=0,
            xmit_sec=base_ntp + 1,
            xmit_frac=0,
        )
        address = ("10.0.0.1", 123)
        originate_time = 5000.0
        destination_time = 5002.0

        result = self.client._parse_response(packet, address, originate_time, destination_time)

        # offset = ((5001-5000) + (5001-5002))/2 = (1 + -1)/2 = 0
        self.assertAlmostEqual(result.clock_offset, 0.0, places=5)
        # delay = (5002-5000) - (5001-5001) = 2 - 0 = 2
        self.assertAlmostEqual(result.roundtrip_delay, 2.0, places=5)

    def test_leap_indicator_alarm(self):
        # byte1 = 0xE4 => LI=3, VN=4, Mode=4
        packet = self._build_ntp_packet(byte1=0xE4)
        address = ("10.0.0.1", 123)

        result = self.client._parse_response(packet, address, 0.0, 0.0)

        self.assertEqual(result.leap_indicator, 3)

    def test_result_is_ntpresult_dataclass(self):
        packet = self._build_ntp_packet()
        address = ("10.0.0.1", 123)

        result = self.client._parse_response(packet, address, 0.0, 0.0)

        # Verify all expected fields exist
        self.assertTrue(hasattr(result, "server"))
        self.assertTrue(hasattr(result, "address"))
        self.assertTrue(hasattr(result, "leap_indicator"))
        self.assertTrue(hasattr(result, "version"))
        self.assertTrue(hasattr(result, "mode"))
        self.assertTrue(hasattr(result, "stratum"))
        self.assertTrue(hasattr(result, "poll"))
        self.assertTrue(hasattr(result, "precision"))
        self.assertTrue(hasattr(result, "root_delay"))
        self.assertTrue(hasattr(result, "root_dispersion"))
        self.assertTrue(hasattr(result, "reference_id"))
        self.assertTrue(hasattr(result, "reference_time"))
        self.assertTrue(hasattr(result, "originate_time"))
        self.assertTrue(hasattr(result, "receive_time"))
        self.assertTrue(hasattr(result, "transmit_time"))
        self.assertTrue(hasattr(result, "destination_time"))
        self.assertTrue(hasattr(result, "clock_offset"))
        self.assertTrue(hasattr(result, "roundtrip_delay"))


if __name__ == "__main__":
    unittest.main()
