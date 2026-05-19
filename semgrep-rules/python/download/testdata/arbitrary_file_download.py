# 综合测试：Python 文件下载 / 任意文件读取 漏洞各种组件场景
# 注意：这个文件只用于 semgrep 规则测试，不要求能实际运行

import os
import os.path
import pathlib
import pickle
import shutil
import subprocess
import sys
import tarfile
import zipfile
from pathlib import Path

import aiofiles
import bottle
import flask
import pandas as pd
import yaml
from aiohttp import web
from django.http import FileResponse, HttpResponse
from fastapi import FastAPI, Query
from flask import request, send_file, send_from_directory
from starlette.responses import FileResponse as StarletteFileResponse


app = flask.Flask(__name__)


# ============================================================
# Flask
# ============================================================

@app.route("/dl1")
def flask_args():
    name = request.args.get("file")
    # ruleid: python-download-arbitrary-file
    return open(name).read()


@app.route("/dl2")
def flask_send_file():
    f = flask.request.args.get("file")
    # ruleid: python-download-arbitrary-file
    return send_file(f)


@app.route("/dl3")
def flask_send_from_directory():
    name = request.args["name"]
    # ruleid: python-download-arbitrary-file
    return send_from_directory("/var/data", name)


@app.route("/dl4")
def flask_path_join():
    name = request.form.get("name")
    # ruleid: python-download-arbitrary-file
    return open(os.path.join("/var/data/", name)).read()


@app.route("/dl5")
def flask_pathlib():
    p = request.headers.get("X-Path")
    # ruleid: python-download-arbitrary-file
    return pathlib.Path(p).read_bytes()


# ============================================================
# Django
# ============================================================

def django_view(request):
    name = request.GET.get("file")
    # ruleid: python-download-arbitrary-file
    return FileResponse(open("/var/data/" + name, "rb"))


def django_post(request):
    name = request.POST["name"]
    # ruleid: python-download-arbitrary-file
    return HttpResponse(open(name, "rb").read(), content_type="application/octet-stream")


def django_meta(request):
    raw = request.META.get("HTTP_X_FILE")
    # ruleid: python-download-arbitrary-file
    return open(raw).read()


# ============================================================
# FastAPI / Starlette
# ============================================================

api = FastAPI()


@api.get("/download")
def fastapi_query(name: str):
    # ruleid: python-download-arbitrary-file
    return StarletteFileResponse(name)


@api.get("/file/{filename}")
def fastapi_path(filename: str):
    # ruleid: python-download-arbitrary-file
    return open("/var/data/" + filename).read()


# ============================================================
# Tornado
# ============================================================

class TornadoHandler:
    def get(self):
        f = self.get_argument("file")
        # ruleid: python-download-arbitrary-file
        with open(f) as fp:
            return fp.read()


# ============================================================
# Bottle
# ============================================================

@bottle.route("/dl")
def bottle_route():
    name = bottle.request.query.get("name")
    # ruleid: python-download-arbitrary-file
    return open(name).read()


# ============================================================
# aiohttp
# ============================================================

async def aiohttp_handler(request):
    name = request.match_info.get("file")
    # ruleid: python-download-arbitrary-file
    async with aiofiles.open(name, "rb") as fp:
        return web.Response(body=await fp.read())


# ============================================================
# 压缩 / 序列化
# ============================================================

@app.route("/zip")
def flask_zip():
    name = request.args.get("name")
    # ruleid: python-download-arbitrary-file
    z = zipfile.ZipFile(name)
    z.close()


@app.route("/tar")
def flask_tar():
    name = request.args.get("name")
    # ruleid: python-download-arbitrary-file
    t = tarfile.open(name)
    t.close()


@app.route("/pickle")
def flask_pickle():
    name = request.args.get("name")
    # ruleid: python-download-arbitrary-file
    return pickle.load(open(name, "rb"))


@app.route("/yaml")
def flask_yaml():
    name = request.args.get("name")
    # ruleid: python-download-arbitrary-file
    return yaml.safe_load(open(name))


# ============================================================
# pandas / numpy
# ============================================================

@app.route("/csv")
def flask_csv():
    name = request.args.get("name")
    # ruleid: python-download-arbitrary-file
    return pd.read_csv(name).to_json()


# ============================================================
# 子进程读取
# ============================================================

@app.route("/cat")
def flask_cat():
    name = request.args.get("name")
    # ruleid: python-download-arbitrary-file
    return subprocess.check_output(["cat", name])


# ============================================================
# 命令行 / 环境变量
# ============================================================

def cli_arg():
    # ruleid: python-download-arbitrary-file
    with open(sys.argv[1]) as fp:
        return fp.read()


def env_var():
    p = os.environ.get("FILE_PATH")
    # ruleid: python-download-arbitrary-file
    return open(p).read()


# ============================================================
# 安全写法（不应被命中）
# ============================================================

@app.route("/safe1")
def safe_basename():
    name = request.args.get("file")
    # ok: python-download-arbitrary-file
    return open(os.path.join("/var/data/", os.path.basename(name))).read()


@app.route("/safe2")
def safe_path_name():
    name = request.args.get("file")
    safe = pathlib.Path(name).name
    # ok: python-download-arbitrary-file
    return open(os.path.join("/var/data/", safe)).read()


@app.route("/safe3")
def safe_int():
    fid = int(request.args.get("id"))
    # ok: python-download-arbitrary-file
    return open(f"/var/data/{fid}.bin").read()


@app.route("/safe4")
def safe_fixed():
    # ok: python-download-arbitrary-file
    return open("/var/data/report.csv").read()


@app.route("/safe5")
def safe_custom_validator():
    name = request.args.get("file")
    safe = validate_path(name)
    # ok: python-download-arbitrary-file
    return open(os.path.join("/var/data/", safe)).read()


def validate_path(p):
    if ".." in p or p.startswith("/"):
        raise ValueError("invalid path")
    return p
