"""Python XXE 测试用例（lxml + 标准库 sax / minidom / xmlrpc）。"""
# 仅用于 semgrep 规则验证

import xml.sax
import xml.sax.handler
import xml.dom.minidom
import xml.dom.pulldom
import xmlrpc.server

import lxml.etree
from lxml import etree

import defusedxml.lxml
import defusedxml.sax
import defusedxml.minidom


# ============= lxml 默认 parser =============

def lxml_parse(path):
    # ruleid: python-xxe-lxml-default
    return lxml.etree.parse(path)


def lxml_fromstring(data):
    # ruleid: python-xxe-lxml-default
    return lxml.etree.fromstring(data)


def etree_parse(path):
    # ruleid: python-xxe-lxml-default
    return etree.parse(path)


def etree_iter(path):
    # ruleid: python-xxe-lxml-default
    return list(etree.iterparse(path))


# ============= lxml 不安全 parser 配置 =============

def lxml_unsafe_resolve():
    # ruleid: python-xxe-lxml-unsafe-parser
    p = lxml.etree.XMLParser(resolve_entities=True)
    return p


def lxml_unsafe_loaddtd():
    # ruleid: python-xxe-lxml-unsafe-parser
    p = etree.XMLParser(load_dtd=True, resolve_entities=False)
    return p


def lxml_unsafe_network():
    # ruleid: python-xxe-lxml-unsafe-parser
    p = etree.XMLParser(no_network=False, resolve_entities=False)
    return p


# ============= xml.sax =============

def sax_make():
    # ruleid: python-xxe-sax
    p = xml.sax.make_parser()
    return p


def sax_parsestring(data):
    # ruleid: python-xxe-sax
    xml.sax.parseString(data, xml.sax.handler.ContentHandler())


# ============= minidom / pulldom =============

def minidom_parse(path):
    # ruleid: python-xxe-minidom-pulldom
    return xml.dom.minidom.parse(path)


def minidom_parsestring(data):
    # ruleid: python-xxe-minidom-pulldom
    return xml.dom.minidom.parseString(data)


def pulldom_parse(path):
    # ruleid: python-xxe-minidom-pulldom
    return xml.dom.pulldom.parse(path)


# ============= xmlrpc =============

def xmlrpc_server():
    # ruleid: python-xxe-xmlrpc-server
    s = xmlrpc.server.SimpleXMLRPCServer(("127.0.0.1", 8000))
    return s


# ============= 安全写法（不应被命中） =============

def safe_defused_lxml(path):
    # ok: python-xxe-lxml-default
    return defusedxml.lxml.parse(path)


def safe_defused_sax(data):
    # ok: python-xxe-sax
    defusedxml.sax.parseString(data, xml.sax.handler.ContentHandler())


def safe_defused_minidom(path):
    # ok: python-xxe-minidom-pulldom
    return defusedxml.minidom.parse(path)


def safe_lxml_with_safe_parser(path):
    safe_parser = etree.XMLParser(
        resolve_entities=False,
        no_network=True,
        load_dtd=False,
        dtd_validation=False,
    )
    # ok: python-xxe-lxml-default
    return etree.parse(path, safe_parser)
