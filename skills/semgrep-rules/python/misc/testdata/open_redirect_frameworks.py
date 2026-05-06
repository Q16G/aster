from django.shortcuts import redirect as django_redirect
from fastapi.responses import RedirectResponse
from flask import redirect
from flask import url_for


def insecure_flask(request):
    # ruleid: python-misc-open-redirect-frameworks
    return redirect(request.args.get("next"))


def insecure_django(request):
    target = request.GET.get("next")
    # ruleid: python-misc-open-redirect-frameworks
    return django_redirect(target)


def insecure_fastapi(request):
    # ruleid: python-misc-open-redirect-frameworks
    return RedirectResponse(request.query_params.get("redirect"))


def safe_redirect():
    # ok: python-misc-open-redirect-frameworks
    return redirect(url_for("dashboard"))
