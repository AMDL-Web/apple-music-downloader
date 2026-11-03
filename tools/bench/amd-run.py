# amd-run.py <amd-src> <codec> <url>...: run AppleMusicDecrypt v3's downloader headless in the cwd and wait for every task.
import asyncio, os, sys, time
src, codec, urls = sys.argv[1], sys.argv[2], sys.argv[3:]
sys.path.insert(0, src)
loop = asyncio.new_event_loop(); asyncio.set_event_loop(loop)
from creart import add_creator, it
from src.logger import LoggerCreator; add_creator(LoggerCreator)
from src.config import ConfigCreator; add_creator(ConfigCreator)
from src.api import APICreator; add_creator(APICreator)
from src.wrapper import WrapperCreator; add_creator(WrapperCreator)
from src.decrypt import DecryptorCreator; add_creator(DecryptorCreator)
from src.measurer import MeasurerCreator, Measurer; add_creator(MeasurerCreator)
from src.tui.task_tree import TaskTreeCreator; add_creator(TaskTreeCreator)
from src.cmd import InteractiveShell
import src.utils as U
shell = InteractiveShell(loop, legacy_ui=True)
async def go():
    t0 = time.monotonic()
    await shell.do_download(urls, codec, False, None, False)
    await asyncio.sleep(0.5)
    while U.background_tasks or it(Measurer).tasks_count() > 0:
        await asyncio.sleep(0.2)
    print(f"AMD-DONE {time.monotonic()-t0:.1f}s", flush=True)
loop.run_until_complete(go())
