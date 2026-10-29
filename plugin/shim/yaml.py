"""PyYAML's place in the Portali plugin, which Cowork runs with no packages installed.

build.py ships every YAML file the runner reads as JSON (YAML's subset): steps files as
safe_load read them, rules tables as BaseLoader reads them (every scalar a string). So
these two calls are all run.py and rules.py need, and they never see real YAML.
"""

import json


class BaseLoader:
    pass


def safe_load(text):
    return json.loads(text)


def load(text, Loader=None):
    return json.loads(text)
