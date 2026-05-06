from flask import render_template_string


def insecure(request):
    # ruleid: python-ssti-render-template-string
    return render_template_string(request.args.get("tpl"))


def insecure_assigned(request):
    template = request.form.get("template")
    # ruleid: python-ssti-render-template-string
    return render_template_string(template)


def safe():
    # ok: python-ssti-render-template-string
    return render_template_string("<p>trusted</p>")
