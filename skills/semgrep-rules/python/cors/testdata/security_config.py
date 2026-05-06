from django.views.decorators.csrf import csrf_exempt
from django.utils.decorators import method_decorator
from flask_cors import CORS
from markupsafe import escape
import jwt
import os
import pathlib
from werkzeug.utils import secure_filename


def insecure_cors(app):
    # ruleid: python-cors-wildcard-credentials
    CORS(app, origins="*", supports_credentials=True)


def default_wildcard_cors(app):
    # ruleid: python-cors-wildcard-credentials
    CORS(app, supports_credentials=True)


def safe_cors(app):
    # ok: python-cors-wildcard-credentials
    CORS(app, origins=["https://example.com"], supports_credentials=True)


@csrf_exempt
def exempt_view(request):
    # ruleid: python-django-csrf-exempt
    return request


@method_decorator(csrf_exempt, name="dispatch")
class ExemptView:
    pass


def disable_wtf_csrf(app):
    # ruleid: python-flask-wtf-csrf-disabled
    app.config["WTF_CSRF_ENABLED"] = False


def safe_wtf_csrf(app):
    # ok: python-flask-wtf-csrf-disabled
    app.config["WTF_CSRF_ENABLED"] = True


# ruleid: python-hardcoded-secret-key
SECRET_KEY = "super-secret"


def hardcoded_secret(app, payload, token):
    # ruleid: python-hardcoded-secret-key
    app.config["SECRET_KEY"] = "another-secret"
    # ruleid: python-hardcoded-secret-key
    jwt.encode(payload, "jwt-secret", algorithm="HS256")
    # ruleid: python-hardcoded-secret-key
    jwt.decode(token, "jwt-secret", algorithms=["HS256"])


def safe_secret(app, payload):
    # ok: python-hardcoded-secret-key
    app.config["SECRET_KEY"] = os.environ["SECRET_KEY"]
    return jwt.encode(payload, os.environ["JWT_SECRET"], algorithm="HS256")


def insecure_upload(file, upload_dir):
    # ruleid: python-upload-unsafe-filename-path
    file.save(os.path.join(upload_dir, file.filename))
    # ruleid: python-upload-unsafe-filename-path
    return pathlib.Path(upload_dir) / file.filename


def safe_upload(file, upload_dir):
    # ok: python-upload-unsafe-filename-path
    file.save(os.path.join(upload_dir, secure_filename(file.filename)))
