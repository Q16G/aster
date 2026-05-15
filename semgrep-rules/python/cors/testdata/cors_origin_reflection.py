from django.http import HttpResponse


# ruleid: python-cors-origin-reflection
def reflect_origin_inline(request):
    response = HttpResponse()
    response["Access-Control-Allow-Origin"] = request.META["HTTP_ORIGIN"]
    return response


# ruleid: python-cors-origin-reflection
def reflect_origin_variable(request):
    origin = request.META["HTTP_ORIGIN"]
    response = HttpResponse()
    response["Access-Control-Allow-Origin"] = origin
    return response


# ok: python-cors-origin-reflection
def static_origin(request):
    response = HttpResponse()
    response["Access-Control-Allow-Origin"] = "https://trusted.example.com"
    return response


# ok: python-cors-origin-reflection
def wildcard_origin(request):
    response = HttpResponse()
    response["Access-Control-Allow-Origin"] = "*"
    return response
