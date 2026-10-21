import argparse
import ast
import base64
import hashlib
import lzma
import struct
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument('reference', type=Path)
options = parser.parse_args()
source = options.reference.read_text(encoding='utf-8')
tree = ast.parse(source)
values = {}
for node in tree.body:
    if isinstance(node, ast.Assign):
        for target in node.targets:
            if isinstance(target, ast.Name) and target.id in {'_BRANCH_ONE_B85', 'SV2', 's_box', 'branch_2_orders'}:
                value = node.value
                if isinstance(value, ast.Call) and isinstance(value.func, ast.Attribute):
                    if value.func.attr == 'fromhex':
                        values[target.id] = bytes.fromhex(ast.literal_eval(value.args[0]))
                    elif value.func.attr == 'join':
                        values[target.id] = b''.join(ast.literal_eval(value.args[0]))
                else:
                    values[target.id] = ast.literal_eval(value)
blob = values['s_box'] + struct.pack('<64I', *values['SV2']) + values['branch_2_orders']
blob += lzma.decompress(base64.b85decode(values['_BRANCH_ONE_B85']))
expected_hash = '059a54341a1f7e9b78243b144a79677331b44c7896d4b9dae77fcf0e6e1075e6'
if len(values['s_box']) != 1024 or len(values['branch_2_orders']) != 320 or len(blob) != 83536:
    raise SystemExit('红果签名固定表长度与参考不符，未写入文件')
if hashlib.sha256(blob).hexdigest() != expected_hash:
    raise SystemExit('红果签名固定表摘要与参考不符，未写入文件')
output = Path(__file__).resolve().parents[1] / 'native' / 'core' / 'hongguo_sign_tables.bin'
output.write_bytes(blob)
print(output)
print('sha256=' + hashlib.sha256(blob).hexdigest())
