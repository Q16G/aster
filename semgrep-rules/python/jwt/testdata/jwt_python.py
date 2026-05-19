"""Python JWT 测试用例（algorithm-confusion / claim 校验缺失 / 弱 secret）。"""
# 仅用于 semgrep 规则验证

import jwt

PUBLIC_KEY = open("public.pem", "rb").read()


# ============= algorithm confusion: 不传 algorithms =============

def decode_no_algs(token):
    # ruleid: python-jwt-algorithm-confusion-no-algorithms
    return jwt.decode(token, PUBLIC_KEY)


def decode_kw_no_algs(token):
    # ruleid: python-jwt-algorithm-confusion-no-algorithms
    return jwt.decode(token, key=PUBLIC_KEY)


# ============= 显式接受 alg=none =============

def decode_allows_none(token):
    # ruleid: python-jwt-algorithms-allows-none
    return jwt.decode(token, "", algorithms=["none"])


def decode_mixes_none(token):
    # ruleid: python-jwt-algorithms-allows-none
    return jwt.decode(token, PUBLIC_KEY, algorithms=["RS256", "none"])


# ============= 关闭签名校验 =============

def decode_verify_false(token):
    # ruleid: python-jwt-verify-disabled
    return jwt.decode(token, verify=False)


def decode_options_no_verify(token):
    # ruleid: python-jwt-verify-disabled
    return jwt.decode(token, PUBLIC_KEY, options={"verify_signature": False})


# ============= missing claim validation =============

def decode_no_issuer(token):
    # ruleid: python-jwt-missing-claim-validation
    return jwt.decode(token, PUBLIC_KEY, algorithms=["RS256"])


# ============= 弱 secret =============

def encode_short(payload):
    # ruleid: python-jwt-weak-secret
    return jwt.encode(payload, "secret", algorithm="HS256")


def encode_short_2(payload):
    # ruleid: python-jwt-weak-secret
    return jwt.encode(payload, "my-app-key", algorithm="HS256")


def decode_short(token):
    # ruleid: python-jwt-weak-secret
    return jwt.decode(token, "abc123", algorithms=["HS256"])


# ============= 安全写法（不应被命中） =============

def decode_safe(token):
    # ok: python-jwt-algorithm-confusion-no-algorithms
    # ok: python-jwt-missing-claim-validation
    return jwt.decode(
        token,
        PUBLIC_KEY,
        algorithms=["RS256"],
        issuer="https://issuer.example.com",
        audience="my-api",
    )


LONG_SECRET = "a-very-long-randomly-generated-secret-key-of-32-plus-chars"


def encode_safe(payload):
    # ok: python-jwt-weak-secret
    return jwt.encode(payload, LONG_SECRET, algorithm="HS256")
