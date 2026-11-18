"""Locust entry point that configures the original scenario without editing it."""
import importlib.util
import os
import sys
from pathlib import Path


def load_scenario():
    credentials = {}
    for env_name, config_name in (
        ("AGORA_APP_ID", "app_id"),
        ("AGORA_APP_CERTIFICATE", "app_certificate"),
    ):
        value = os.environ.get(env_name, "").strip()
        try:
            valid = len(value) == 32 and len(bytes.fromhex(value)) == 16
        except ValueError:
            valid = False
        if not valid:
            raise ValueError(f"{env_name} must contain 32 hexadecimal characters")
        credentials[config_name] = value

    scenario_dir = Path(__file__).resolve().parent
    sys.path.insert(0, str(scenario_dir))
    spec = importlib.util.spec_from_file_location("_apaas_original_scenario", scenario_dir / "locustfile.py")
    scenario = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = scenario
    spec.loader.exec_module(scenario)

    # The original module creates the pool during import. Rebuild it after
    # applying credentials, so both users and the cached server token use them.
    scenario.CONFIG.update(credentials)
    scenario.room_pool = scenario.RoomPool()
    scenario.logger.info("Locust adapter applied external credentials and rebuilt the room pool")
    return scenario


scenario = load_scenario()
APaasUser = scenario.APaasUser
