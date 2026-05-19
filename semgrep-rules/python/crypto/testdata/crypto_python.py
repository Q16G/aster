"""Python crypto 弱算法 / 不安全随机 / TLS 跳过验证 测试用例。"""
# 仅用于 semgrep 规则验证

import hashlib
import random
import ssl

import requests
import httpx
from Crypto.Cipher import DES, DES3, ARC4, Blowfish


# ============= 弱算法 =============

def use_des(key):
    # ruleid: python-crypto-weak-algorithm
    return DES.new(key, DES.MODE_ECB)


def use_3des(key):
    # ruleid: python-crypto-weak-algorithm
    return DES3.new(key, DES3.MODE_CBC)


def use_arc4(key):
    # ruleid: python-crypto-weak-algorithm
    return ARC4.new(key)


def use_blowfish(key):
    # ruleid: python-crypto-weak-algorithm
    return Blowfish.new(key, Blowfish.MODE_CBC)


def use_md5(data):
    # ruleid: python-crypto-weak-algorithm
    return hashlib.md5(data)


def use_sha1(data):
    # ruleid: python-crypto-weak-algorithm
    return hashlib.sha1(data)


# ============= 不安全随机 =============

def generate_token():
    # ruleid: python-crypto-insecure-random
    return str(random.randint(100000, 999999))


def generate_session_id():
    # ruleid: python-crypto-insecure-random
    chars = "abcdefghijklmnopqrstuvwxyz0123456789"
    return "".join(random.choice(chars) for _ in range(32))


# ============= TLS 跳过验证 =============

def insecure_get(url):
    # ruleid: python-crypto-tls-unverified
    return requests.get(url, verify=False)


def insecure_post(url, data):
    # ruleid: python-crypto-tls-unverified
    return requests.post(url, json=data, verify=False)


def insecure_httpx(url):
    # ruleid: python-crypto-tls-unverified
    return httpx.get(url, verify=False)


def insecure_ssl():
    # ruleid: python-crypto-tls-unverified
    return ssl._create_unverified_context()


# ============= 安全写法 =============

def safe_hash(data):
    # ok: python-crypto-weak-algorithm
    return hashlib.sha256(data)


def safe_random():
    import secrets
    # ok: python-crypto-insecure-random
    return secrets.token_hex(32)


def safe_request(url):
    # ok: python-crypto-tls-unverified
    return requests.get(url)
