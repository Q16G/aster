from jinja2 import Template


def insecure(request):
    # ruleid: python-ssti-jinja2-template-string
    return Template(request.args.get("tpl")).render()


def insecure_assigned(request):
    template = Template(request.form.get("template"))
    # ruleid: python-ssti-jinja2-template-string
    return template.render()


def safe():
    # ok: python-ssti-jinja2-template-string
    return Template("<p>trusted</p>").render()
