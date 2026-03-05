#! /usr/bin/python
# -*- coding: latin-1 -*-
#
"""
SNTP client

Description:
Uses the SNTP protocol (as specified in RFC 2030)
to contact the server specified in the command line
and report the time as returned by that server.
"""
import socket
import struct
import sys
import time

"""
clock.tix.ch
timeserver.ntp.ch
ntp.metas.ch
swisstime.ethz.ch
"""


if len(sys.argv) > 1:
    server = sys.argv[1]
else:
    server = "129.132.2.21"  # "swisstime.ethz.ch"
    # server = "0.pool.ntp.org"

TIME1970 = 2208988800
version = 4

liText = {
    0: "no warning",
    1: "last minute of current day has 61 sec",
    2: "last minute of current day has 59 sec",
    3: "alarm condition (clock not synchronized)",
}

modeText = {
    0: "reserved",
    1: "symmetric active",
    2: "symmetric passive",
    3: "client",
    4: "server",
    5: "broadcast",
    6: "reserved for NTP control message",
    7: "reserved for private use",
}

stratumText = {
    0: "unspecified or unavailable",
    1: "primary reference (e.g. radio clock)",
    2: "2...15: secondary reference (via NTP or SNTP)",
    16: "16...255: reserved",
}

"""
The message is simplified, i.e. the originate Time is not included
in the message. According to the RFC 2030, it should included in the
transmit time field and then the NTP server send it back.
It is use to ckeck if the received packet is the correct reply to our request.
"""
if version == 3:
    #
    # SNTP Message version 3
    #
    message = b"\x1b" + 47 * b"\0"
elif version == 4:
    #
    # SNTP Message version 4
    #
    message = b"\x23" + 47 * b"\0"
#
#
#
originateTime = time.time()
#
# Connect to the server
#
socket.setdefaulttimeout(20.0)
# print socket.getdefaulttimeout()
client = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
client.sendto(message, (server, 123))
#
# Get the answer
#
data, address = client.recvfrom(1024)
destinationTime = time.time()
client.close()
#
"""
# debug part
data = open('sntp_data.txt', 'r').read()
address = server, 123
"""
if data:
    #
    (
        byte1,
        stratum,
        poll,
        precision,
        rootDelay,
        rootDispersion,
        referenceIdent,
        referenceTime,
        referenceTimeFB,
        originateTime2,
        originateTime2FB,
        receiveTime,
        receiveTimeFB,
        transmitTime,
        transmitTimeFB,
    ) = struct.unpack("!2B2b2i4s8I", data)
    #
    # Decode the first byte
    #
    li = (byte1 & 0xC0) >> 6
    vn = (byte1 & 0x38) >> 3
    mode = byte1 & 0x07
    #
    # Stratum : 1 , 2-15, 16-255
    #
    if stratum == 1:
        pass
    elif stratum > 1 or stratum < 16:
        stratum = 2
    elif stratum > 15:
        stratum = 16
    #
    # Interpret the reference identification
    #
    if stratum == 1:
        referenceIdent = struct.unpack("4s", referenceIdent)
    elif stratum == 2:
        if vn == 3:
            print(referenceIdent)
            referenceIdent = (
                "[32bit IPv4 address %d.%d.%d.%d of the ref src]"
                % struct.unpack("4B", referenceIdent)
            )
        elif vn == 4:
            referenceIdent = (
                "%X%X%X%X [low 32bits of latest TX timestamp of reference src]"
                % tuple([ord(s) for s in referenceIdent])
            )
    #
    # Decode the different time received by the NTP server
    #
    """
    In order to convert fixed point representation number, 
    we take the fractiona part of the number and we devide it by 2**32. 
    """
    receiveTime = receiveTime + (receiveTimeFB / 4294967296) - TIME1970
    referenceTime = referenceTime + (referenceTimeFB / 4294967296) - TIME1970

    originateTime2 = originateTime2 + (originateTime2FB / 4294967296) - TIME1970
    transmitTime = transmitTime + (transmitTimeFB / 4294967296) - TIME1970
    #
    # compute the clock offset and roundtrip delay according to the RFC 2030
    #
    clockOffset = (
        (receiveTime - originateTime) + (transmitTime - destinationTime)
    ) / 2.0
    roundtrip = (destinationTime - originateTime) - (receiveTime - transmitTime)
    #
    # Print the result
    #
    print()
    print("Response received from : %s" % server)
    print("IP address             : %s\nPort                   : %s" % address)
    print()
    print("Header")
    print("-" * 80)
    print("Byte1                  : 0x%X" % (byte1))
    print("  Leap Indicator (LI)  : %i [%s]" % (li, liText[li]))
    print("  Version number (VN)  : %i [NTP/SNTP version number]" % vn)
    print("  Mode                 : %i [%s]" % (mode, modeText[mode]))
    print("Stratum                : %i [%s]" % (stratum, stratumText[stratum]))
    print("Poll interval          : %i" % poll)
    print("Clock Precision        : 2**%i = %1.5E" % (precision, 2 ** precision))
    print("Root Delay             : 0x%08X = %10.5f" % (rootDelay, rootDelay / 65536.0))
    print(
        "Root Dispersion        : 0x%08X = %10.5f"
        % (rootDispersion, rootDispersion / 65536.0)
    )
    print("Reference Identifier   : %s" % referenceIdent)
    print()
    print(
        "Interpreted results, converted to unix epoch (sec since 1970-01-01 00:00:00):"
    )
    print("-" * 80)
    print(
        "Reference Timestamp    : %10.5f [last sync of server clock with ref]"
        % referenceTime
    )
    print(
        "Originate Timestamp    : %10.5f [time request sent by client]" % originateTime
    )
    print(
        "Receive   Timestamp    : %10.5f [time request received by server]"
        % receiveTime
    )
    print("Transmit  Timestamp    : %10.5f [time reply sent by server]" % transmitTime)
    print(
        "Destination Timestamp  : %10.5f [time reply received by client]"
        % destinationTime
    )
    print("-" * 80)
    print()
    print(
        "Net Time UTC           : %s + %0.3f ms"
        % (time.ctime(receiveTime), receiveTime - int(receiveTime))
    )
    print(
        "Local Time UTC         : %s + %0.3f ms"
        % (time.ctime(originateTime), originateTime - int(originateTime))
    )
    print("Clock Offset           : %10.5f " % clockOffset)
    print("Roundtrip Delay        : %10.5f " % roundtrip)
