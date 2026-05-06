import os
from django.core.files.storage import default_storage


def insecure_storage(file_obj):
    # ruleid: python-upload-framework-paths
    return default_storage.save(file_obj.name, file_obj)


def safe_storage(storage, file_obj):
    # ok: python-upload-framework-paths
    return storage.save(storage.get_valid_name(file_obj.name), file_obj)


def insecure_fastapi(upload, directory):
    # ruleid: python-upload-framework-paths
    return open(f"{directory}/{upload.filename}", "wb")


def safe_fastapi(upload, directory):
    # ok: python-upload-framework-paths
    return open(f"{directory}/{os.path.basename(upload.filename)}", "wb")
