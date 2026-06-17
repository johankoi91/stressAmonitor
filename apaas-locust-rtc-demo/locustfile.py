import json
import sys
import time
import random
import gevent
import logging
import threading
import os
import uuid
from datetime import datetime

from typing import Dict, Any
from locust import HttpUser, task, TaskSet, events
from locust.env import Environment
from src.apaas_token_builder import ApaasTokenBuilder

# 配置日志系统
def setup_logging():
    """配置日志系统，同时输出到控制台和文件"""
    # 创建logs目录
    if not os.path.exists('logs'):
        os.makedirs('logs')
    
    # 生成日志文件名（包含时间戳）
    timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
    log_filename = f'logs/apaas_test_{timestamp}.log'
    
    # 配置日志格式
    log_format = '%(asctime)s - %(levelname)s - %(message)s'
    date_format = '%Y-%m-%d %H:%M:%S'
    
    # 创建专用的logger，避免与Locust的日志配置冲突
    logger = logging.getLogger('apaas_test')
    logger.setLevel(logging.DEBUG)
    
    # 清除现有的handlers
    for handler in logger.handlers[:]:
        logger.removeHandler(handler)
    
    # 创建控制台handler
    console_handler = logging.StreamHandler(sys.stdout)
    console_handler.setLevel(logging.INFO)
    console_formatter = logging.Formatter(log_format, date_format)
    console_handler.setFormatter(console_formatter)
    logger.addHandler(console_handler)
    
    # 创建文件handler
    file_handler = logging.FileHandler(log_filename, encoding='utf-8')
    file_handler.setLevel(logging.DEBUG)
    file_formatter = logging.Formatter(log_format, date_format)
    file_handler.setFormatter(file_formatter)
    logger.addHandler(file_handler)
    
    # 防止日志向上传播到根logger
    logger.propagate = False
    
    # 输出日志文件路径
    print(f"日志文件路径: {os.path.abspath(log_filename)}")
    
    return logger

# 初始化日志系统
logger = setup_logging()

# 全局变量用于跟踪用户完成状态
completed_users = 0
total_users = 0  # 将在CONFIG初始化后更新

def format_room_log(message, room_id=None, room_type=None, user_id=None, level="DEBUG", **kwargs):
    """统一的房间日志格式化函数
    
    Args:
        message: 日志消息
        room_id: 房间ID
        room_type: 房间类型
        user_id: 用户ID
        level: 日志级别，支持 "DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"
        **kwargs: 其他参数
    """
    # 构建基础日志信息
    log_parts = [f"{message}"]

    # 添加房间信息
    if room_id:
        log_parts.append(f"房间ID: {room_id}")

    if room_type:
        log_parts.append(f"房间类型: {room_type}")

    if user_id:
        log_parts.append(f"用户ID: {user_id}")

    # 添加其他参数
    for key, value in kwargs.items():
        if value is not None:
            log_parts.append(f"{key}: {value}")

    # 根据级别调用对应的日志方法
    log_message = " | ".join(log_parts)
    level_upper = level.upper()
    
    if level_upper == "DEBUG":
        logger.debug(log_message)
    elif level_upper == "INFO":
        logger.info(log_message)
    elif level_upper == "WARNING":
        logger.warning(log_message)
    elif level_upper == "ERROR":
        logger.error(log_message)
    elif level_upper == "CRITICAL":
        logger.critical(log_message)
    else:
        # 默认使用INFO级别
        logger.info(log_message)

# 本地构建scene token
def build_scene_token(app_id, app_certificate):

    if not app_id or not app_certificate:
        logger.error("Need to set environment variable AGORA_APP_ID and AGORA_APP_CERTIFICATE")
        return

    token = ApaasTokenBuilder.build_app_token(app_id, app_certificate, 6000)
    return token

def build_room_user_token(app_id, app_certificate, room_uuid, user_uuid, role = 1, expiration_in_seconds = 6000):
    if not app_id or not app_certificate:
        logger.error("Need to set environment variable AGORA_APP_ID and AGORA_APP_CERTIFICATE")
        return

    token = ApaasTokenBuilder.build_room_user_token(app_id, app_certificate, room_uuid, user_uuid, role,
                                                    expiration_in_seconds)
    return token

# ------------------------------------------------------------
# 1. 动态配置管理
# ------------------------------------------------------------
class DynamicConfig:
    """动态配置管理类，支持运行时更新配置"""

    def __init__(self):
        # 默认配置
        self._config: Dict[str, Any] = {
            "total_users": 1000,  # 总用户数
            "small_room_capacity": 3,  # 小房间容量
            "spawn_rate": 20,  # 每秒孵化人数
            "big_room_capacity": 0,
            "room_keep_seconds": 60,  # 房间内保持时间
            "app_id": "50b7cb2ccd4f46f7936d0e2a52e56d1d",                # 客户appId
            "app_certificate": "75e3a40a0d654a969b6d35577e634228",            # 客户secret
        }

        # 从环境变量获取鉴权参数
        self._update_from_env()

        # 从命令行参数获取Locust参数
        self._update_from_locust_args()

        # 验证和调整配置
        self._validate_and_adjust_config()

        # 输出配置信息
        self._print_config()

    def _update_from_env(self):
        """从环境变量更新 App ID 和 App Certificate"""
        app_id = os.getenv("AGORA_APP_ID", "").strip()
        app_certificate = os.getenv("AGORA_APP_CERTIFICATE", "").strip()

        if app_id:
            self._config["app_id"] = app_id
            logger.info("从环境变量 AGORA_APP_ID 设置 app_id")
        else:
            logger.warning("未设置环境变量 AGORA_APP_ID，使用默认 app_id")

        if app_certificate:
            self._config["app_certificate"] = app_certificate
            logger.info("从环境变量 AGORA_APP_CERTIFICATE 设置 app_certificate")
        else:
            logger.warning("未设置环境变量 AGORA_APP_CERTIFICATE，使用默认 app_certificate")

    def _update_from_locust_args(self):
        """从Locust命令行参数更新配置"""
        try:
            import sys

            # 解析命令行参数
            args = sys.argv[1:]  # 跳过脚本名称

            i = 0
            while i < len(args):
                arg = args[i]

                # 处理 -u 或 --users 参数
                if arg in ['-u', '--users']:
                    if i + 1 < len(args) and not args[i + 1].startswith('-'):
                        try:
                            users = int(args[i + 1])
                            if users > 0:
                                self._config["total_users"] = users
                                logger.info(f"从命令行参数设置总用户数: {users}")
                            i += 2
                        except ValueError:
                            logger.warning(f"警告: 无效的用户数参数: {args[i + 1]}")
                            i += 2
                    else:
                        i += 1

                # 处理 -t 或 --run-time 参数
                elif arg in ['-t', '--run-time']:
                    if i + 1 < len(args) and not args[i + 1].startswith('-'):
                        try:
                            run_time_str = args[i + 1]
                            # 解析时间格式 (如: 30s, 5m, 2h)
                            run_time_seconds = self._parse_time_string(run_time_str)
                            if run_time_seconds > 0:
                                # 根据运行时间调整房间保持时间
                                # 运行时间减去一些缓冲时间作为房间保持时间
                                buffer_time = 30  # 30秒缓冲时间
                                room_keep_time = max(10, run_time_seconds - buffer_time)
                                self._config["room_keep_seconds"] = room_keep_time
                                logger.info(
                                    f"从命令行参数设置房间保持时间: {room_keep_time}秒 (基于运行时间: {run_time_str})")
                            i += 2
                        except ValueError:
                            logger.warning(f"警告: 无效的运行时间参数: {args[i + 1]}")
                            i += 2
                    else:
                        i += 1

                # 处理 --spawn-rate 参数
                elif arg in ['-r', '--spawn-rate']:
                    if i + 1 < len(args) and not args[i + 1].startswith('-'):
                        try:
                            spawn_rate = float(args[i + 1])
                            if spawn_rate > 0:
                                self._config["spawn_rate"] = spawn_rate
                                logger.info(f"从命令行参数设置孵化率: {spawn_rate}")
                            i += 2
                        except ValueError:
                            logger.warning(f"警告: 无效的孵化率参数: {args[i + 1]}")
                            i += 2
                    else:
                        i += 1
                else:
                    i += 1

        except Exception as e:
            logger.error(f"解析命令行参数时出错: {e}")

    def _parse_time_string(self, time_str: str) -> int:
        """解析时间字符串，支持 s(秒), m(分钟), h(小时) 格式"""
        time_str = time_str.lower().strip()

        if time_str.endswith('s'):
            return int(time_str[:-1])
        elif time_str.endswith('m'):
            return int(time_str[:-1]) * 60
        elif time_str.endswith('h'):
            return int(time_str[:-1]) * 3600
        else:
            # 默认按秒处理
            return int(time_str)

    def _validate_and_adjust_config(self):
        """验证和调整配置"""
        # 确保总用户数大于0
        if self._config["total_users"] <= 0:
            logger.warning("警告: 总用户数必须大于0，使用默认值1000")
            self._config["total_users"] = 1000

        # 确保spawn_rate大于0
        if self._config["spawn_rate"] <= 0:
            logger.warning("警告: 孵化率必须大于0，使用默认值20")
            self._config["spawn_rate"] = 20

        # 确保小房间容量大于0
        if self._config["small_room_capacity"] <= 0:
            logger.warning("警告: 小房间容量必须大于0，使用默认值3")
            self._config["small_room_capacity"] = 3

        # 根据total_users自动设置big_room_capacity和small_rooms_target_users
        total = self._config["total_users"]

        # 强制分配模式：大房间1/4，小房间3/4
        big_room_users = 500  # 总数的1/4
        small_room_users = total - big_room_users  # 剩余的3/4

        # 设置大房间容量（等于大房间目标用户数）
        self._config["big_room_capacity"] = big_room_users

        # 设置小房间目标用户数
        self._config["small_rooms_target_users"] = small_room_users

        logger.info(f"根据总用户数 {total} 自动设置:")
        logger.info(f"  大房间容量: {big_room_users} (1/4)")
        logger.info(f"  小房间目标用户数: {small_room_users} (3/4)")

    def _print_config(self):
        """输出当前配置"""
        logger.info("=" * 60)
        logger.info("当前配置:")
        logger.info("=" * 60)
        logger.info(f"总用户数: {self._config['total_users']}")
        logger.info(f"每秒孵化用户数: {self._config['spawn_rate']}")
        logger.info(f"房间保持时间: {self._config['room_keep_seconds']}秒")
        logger.info(f"大房间容量: {self._config['big_room_capacity']} (自动计算: 总用户数的1/4)")
        logger.info(f"小房间容量: {self._config['small_room_capacity']}")
        logger.info(f"小房间目标用户数: {self._config['small_rooms_target_users']} (自动计算: 总用户数的3/4)")
        logger.info("=" * 60)

    def get(self, key: str, default=None):
        """获取配置值"""
        return self._config.get(key, default)

    def set(self, key: str, value):
        """设置配置值"""
        self._config[key] = value

    def update(self, updates: Dict[str, Any]):
        """批量更新配置"""
        self._config.update(updates)
        # 重新验证和调整
        self._validate_and_adjust_config()

    def get_all(self) -> Dict[str, Any]:
        """获取所有配置"""
        return self._config.copy()

# 创建全局配置实例
CONFIG = DynamicConfig()


# ------------------------------------------------------------
# 2. 房间池（线程安全）- 支持虚拟房间ID到真实房间ID的映射
# ------------------------------------------------------------
class RoomPool:
    def __init__(self):
        self.lock = threading.Lock()
        self.user_counter = 0  # 已分配用户数
        self.left_counter = 0  # 已离开用户数

        # 虚拟房间配置
        self.virtual_rooms = {}
        self._init_virtual_rooms()
        self.app_id = CONFIG.get("app_id")
        self.app_certificate = CONFIG.get("app_certificate")
        self.server_token = build_scene_token(self.app_id, self.app_certificate)

    def _init_virtual_rooms(self):
        """根据配置初始化虚拟房间"""
        with self.lock:
            # 获取进程ID和时间戳，进一步提高唯一性
            process_id = os.getpid()
            timestamp = int(time.time() * 1000)  # 毫秒级时间戳
            
            # 创建大房间虚拟ID，使用UUID防止重复
            big_room_virtual_id = f"virtual_big_{str(uuid.uuid4()).replace('-', '')}"
            self.virtual_rooms[big_room_virtual_id] = {
                "virtual_id": big_room_virtual_id,
                "real_id": None,  # 真实房间ID，由第一个用户创建时设置
                "type": "big",
                "capacity": CONFIG.get("big_room_capacity"),
                "target_users": CONFIG.get("big_room_capacity"),  # 大房间目标用户数等于容量
                "users": set(),
                "online_users": set(),  # 在线用户集合
                "creator": None,
                "is_created": False  # 是否已创建真实房间
            }

            # 创建小房间虚拟ID
            small_room_count = (CONFIG.get("small_rooms_target_users") + CONFIG.get(
                "small_room_capacity") - 1) // CONFIG.get("small_room_capacity")

            for i in range(small_room_count):
                # 为每个小房间生成独立的UUID和时间戳
                room_uuid = str(uuid.uuid4()).replace('-', '')
                room_timestamp = int(time.time() * 1000) + i  # 每个房间递增1毫秒
                virtual_id = f"small_{room_uuid}_{room_timestamp}_{i}"
                self.virtual_rooms[virtual_id] = {
                    "virtual_id": virtual_id,
                    "real_id": None,  # 真实房间ID，由第一个用户创建时设置
                    "type": "small",
                    "capacity": CONFIG.get("small_room_capacity"),
                    "target_users": min(CONFIG.get("small_room_capacity"),
                                        CONFIG.get("small_rooms_target_users") - i * CONFIG.get("small_room_capacity")),
                    "users": set(),
                    "online_users": set(),  # 在线用户集合
                    "creator": None,
                    "is_created": False  # 是否已创建真实房间
                }

            # 这个日志总是输出，不受debug控制
            logger.info(f"初始化虚拟房间完成: 1个大房间, {small_room_count}个小房间")

    def get_server_token(self) -> str:
        return self.server_token

    def get_next_virtual_room(self, user_id: str) -> tuple:
        """获取下一个可用的虚拟房间，返回(virtual_id, is_creator)"""
        # 在锁外准备日志信息
        log_info = None
        error_info = None

        with self.lock:
            self.user_counter += 1

            # 强制分配模式：按目标人数分配
            if self.user_counter <= CONFIG.get("big_room_capacity"):
                room_type = "big"
            else:
                room_type = "small"

            # 查找对应类型的可用虚拟房间
            for virtual_id, room_info in self.virtual_rooms.items():
                if room_info["type"] == room_type and len(room_info["users"]) < room_info["capacity"]:
                    # 检查是否是该房间的第一个用户
                    is_creator = len(room_info["users"]) == 0

                    # 添加用户到虚拟房间
                    room_info["users"].add(user_id)

                    # 如果是创建者，设置creator
                    if is_creator:
                        room_info["creator"] = user_id

                    # 准备日志信息，但不在这里调用format_room_log
                    log_info = {
                        "virtual_id": virtual_id,
                        "room_type": room_type,
                        "user_id": user_id,
                        "is_creator": is_creator,
                        "current_users": len(room_info["users"]),
                        "capacity": room_info["capacity"]
                    }

                    return virtual_id, is_creator

            # 如果没有找到可用的房间，准备错误信息
            error_info = {"room_type": room_type, "user_id": user_id}
            return None, False

        # 在锁外输出日志
        if log_info:
            format_room_log(
                f"分配虚拟房间 - {'创建者' if log_info['is_creator'] else '参与者'}",
                room_id=log_info["virtual_id"],
                room_type=log_info["room_type"],
                user_id=log_info["user_id"],
                current_users=log_info["current_users"],
                capacity=log_info["capacity"]
            )
        elif error_info:
            format_room_log(f"错误：没有找到可用的{error_info['room_type']}房间", user_id=error_info["user_id"])

    def get_virtual_room_info(self, virtual_id: str) -> Dict:
        """获取虚拟房间信息"""
        with self.lock:
            if virtual_id in self.virtual_rooms:
                room = self.virtual_rooms[virtual_id]
                return {
                    "virtual_id": virtual_id,
                    "real_id": room["real_id"],
                    "type": room["type"],
                    "capacity": room["capacity"],
                    "current_users": len(room["users"]),
                    "online_users": len(room["online_users"]),
                    "available_slots": room["capacity"] - len(room["users"]),
                    "is_created": room["is_created"],
                    "creator": room["creator"]
                }
            return None

    def set_real_room_id(self, virtual_id: str, real_id: str, creator_id: str) -> bool:
        """设置真实房间ID（由创建者调用）"""
        # 在锁外准备日志信息
        log_info = None

        with self.lock:
            if virtual_id in self.virtual_rooms:
                room = self.virtual_rooms[virtual_id]
                if room["creator"] == creator_id and not room["is_created"]:
                    room["real_id"] = real_id
                    room["is_created"] = True

                    # 准备日志信息，但不在这里调用format_room_log
                    log_info = {
                        "room_id": virtual_id,
                        "real_room_id": real_id,
                        "user_id": creator_id,
                        "room_type": room["type"]
                    }

                    if room["type"] == "big":
                        logger.info(f"=================大房间创建成功===============: {real_id}")

                    return True
            return False

        # 在锁外输出日志
        if log_info:
            format_room_log(
                "设置真实房间ID",
                room_id=log_info["room_id"],
                real_room_id=log_info["real_room_id"],
                user_id=log_info["user_id"],
                room_type=log_info["room_type"]
            )

    def get_real_room_id(self, virtual_id: str) -> str:
        """获取真实房间ID"""
        with self.lock:
            if virtual_id in self.virtual_rooms:
                return self.virtual_rooms[virtual_id]["real_id"]
            return None

    def is_room_created(self, virtual_id: str) -> bool:
        """检查房间是否已创建"""
        with self.lock:
            if virtual_id in self.virtual_rooms:
                return self.virtual_rooms[virtual_id]["is_created"]
            return False

    def leave_virtual_room(self, virtual_id: str, user_id: str):
        """离开虚拟房间"""
        # 在锁外准备日志信息
        log_info = None

        with self.lock:
            self.left_counter += 1
            if virtual_id in self.virtual_rooms:
                room = self.virtual_rooms[virtual_id]
                room["users"].discard(user_id)
                current_users = len(room["users"])
                capacity = room["capacity"]
                available_slots = capacity - current_users

                # 准备日志信息，但不在这里调用format_room_log
                log_info = {
                    "room_id": virtual_id,
                    "user_id": user_id,
                    "left_counter": self.left_counter,
                    "current_users": current_users,
                    "capacity": capacity,
                    "available_slots": available_slots
                }

            # 注意：退出逻辑现在在 _finish_user 方法中处理

        # 在锁外输出日志
        if log_info:
            format_room_log(
                "用户离开虚拟房间",
                room_id=log_info["room_id"],
                user_id=log_info["user_id"],
                left_counter=log_info["left_counter"],
                current_users=log_info["current_users"],
                capacity=log_info["capacity"],
                available_slots=log_info["available_slots"]
            )

    def user_online(self, virtual_id: str, user_id: str) -> bool:
        """用户上线，返回是否达到房间容量"""
        with self.lock:
            if virtual_id in self.virtual_rooms:
                room = self.virtual_rooms[virtual_id]
                # 检查用户是否在房间中
                if user_id in room["users"]:
                    # 添加到在线用户集合
                    room["online_users"].add(user_id)
                    current_online_users = len(room["online_users"])
                    capacity = room["capacity"]
                    
                    # 检查是否达到容量
                    if current_online_users == capacity:
                        # 记录当前在线用户数
                        format_room_log(
                            f"用户上线 - 当前在线用户数: {current_online_users}/{capacity}",
                            room_id=virtual_id,
                            room_type=room["type"],
                            user_id=user_id,
                            current_online_users=current_online_users,
                            capacity=capacity,
                            level="INFO"
                        )
                        return True
            return False

    def get_room_stats(self) -> Dict:
        """获取房间统计信息"""
        with self.lock:
            stats = {
                "total_virtual_rooms": len(self.virtual_rooms),
                "big_rooms": 0,
                "small_rooms": 0,
                "created_rooms": 0,
                "total_users": self.user_counter,
                "left_users": self.left_counter
            }

            for room in self.virtual_rooms.values():
                if room["type"] == "big":
                    stats["big_rooms"] += 1
                else:
                    stats["small_rooms"] += 1

                if room["is_created"]:
                    stats["created_rooms"] += 1

            return stats
room_pool = RoomPool()

# ------------------------------------------------------------
# 4. 用户类（每个用户都要执行一次完整的生命周期）
# ------------------------------------------------------------
class APaasUser(HttpUser):
    def __init__(self, environment):
        super().__init__(environment)
        self.is_creator = None              # 是否是房间创建者
        self.virtual_room_id = None         # 初始分配的虚拟房间ID
        self.life_cycle_executed = None     # 生命周期是否已经执行过
        self.app_id = CONFIG.get("app_id")
        self.app_certificate = CONFIG.get("app_certificate")
        self.userUuid = None                # 真实的用户ID
        self.roomUuid = None                # 真实的房间ID
        self.room_type = None               # 用户加入的房间类型
        self.streamUuid = None              # 加入房间分配的流ID
        self.token = None                   # scene分配的token

    def on_start(self) -> None:
        # 使用UUID生成用户ID，防止重复
        self.userUuid = f"user_{str(uuid.uuid4()).replace('-', '')}"
        self.life_cycle_executed = False  # 添加执行标志

        # 获取虚拟房间ID和创建者标识
        self.virtual_room_id, self.is_creator = room_pool.get_next_virtual_room(self.userUuid)
        if not self.virtual_room_id:
            logger.error(f"错误：无法分配虚拟房间，停止用户 {self.userUuid}")
            # self._finish_user()
            return

        # 获取虚拟房间信息
        virtual_room_info = room_pool.get_virtual_room_info(self.virtual_room_id)
        if not virtual_room_info:
            logger.error(f"错误：无法获取虚拟房间信息，停止用户 {self.userUuid}")
            # self._finish_user()
            return

        self.room_type = virtual_room_info["type"]
        self.roomUuid = None  # 真实房间ID，稍后设置

        # 如果是创建者，需要创建真实房间
        if self.is_creator:
            format_room_log(
                "用户是房间创建者，需要创建真实房间",
                room_id=self.virtual_room_id,
                room_type=self.room_type,
                user_id=self.userUuid
            )
        else:
            # 如果不是创建者，等待真实房间创建完成
            format_room_log(
                "用户是房间参与者，等待真实房间创建",
                room_id=self.virtual_room_id,
                room_type=self.room_type,
                user_id=self.userUuid
            )
        self.streamUuid = None

    def on_stop(self) -> None:
        return
        # 检查是否所有用户都已完成
        # if completed_users >= total_users:
            # 这个日志总是输出，不受debug控制
            # print("所有用户已完成，准备结束应用程序")
            # 延迟一点时间确保所有日志都能输出
            # gevent.spawn_later(2, sys.exit, 0)
            # gevent.spawn_later(0, sys.exit, 0)

    @task
    def run_once(self):
        """执行完整的加入房间流程"""
        # 检查是否已经执行过
        if self.life_cycle_executed:
            return

        self.life_cycle_executed = True  # 标记为已执行
        format_room_log("开始加入房间流程", room_id=self.virtual_room_id, user_id=self.userUuid,
                        room_type=self.room_type)

        # 依次执行加入房间的各个步骤
        try:
            if self.is_creator:
                # 创建者：创建真实房间
                format_room_log("步骤1: 创建者：开始创建真实房间", room_id=self.virtual_room_id, user_id=self.userUuid,
                                room_type=self.room_type)
                self.create_scene_room()

                # 检查房间是否创建成功
                if not self.roomUuid:
                    self.start_scene_room(room_pool.get_server_token())
                    format_room_log("创建真实房间失败", room_id=self.virtual_room_id, user_id=self.userUuid)
                    return False
            else:
                # 参与者：等待真实房间创建完成
                format_room_log("步骤1: 参与者：等待真实房间创建", room_id=self.virtual_room_id, user_id=self.userUuid,
                                room_type=self.room_type)
                max_wait_time = 30  # 最大等待30秒
                wait_interval = 0.5  # 每0.5秒检查一次
                waited_time = 0

                while waited_time < max_wait_time:
                    self.roomUuid = room_pool.get_real_room_id(self.virtual_room_id)
                    if self.roomUuid:
                        format_room_log("步骤1: 获取到真实房间ID", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                        user_id=self.userUuid)
                        break
                    time.sleep(wait_interval)
                    waited_time += wait_interval

                if not self.roomUuid:
                    format_room_log("等待真实房间创建超时", room_id=self.virtual_room_id, user_id=self.userUuid)
                    return False

            # 步骤2: 获取token
            format_room_log("步骤2: 获取token", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                            user_id=self.userUuid, room_type=self.room_type)
            self.get_scene_token()

            # 步骤3: 加入房间
            format_room_log("步骤3: 进入房间", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                            user_id=self.userUuid, room_type=self.room_type)
            self.entry_scene_room()

            # 检查是否成功获取streamUuid
            if not self.streamUuid:
                format_room_log("进入房间失败，未获取到streamUuid", room_id=self.virtual_room_id,
                                real_room_id=self.roomUuid, user_id=self.userUuid)
                return False

            # 步骤4: 上线状态
            format_room_log("步骤4: 上线状态", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                            user_id=self.userUuid, room_type=self.room_type)
            self.scene_online()

            # 步骤5: 更新音频流
            format_room_log("步骤5: 更新音频流", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                            user_id=self.userUuid, room_type=self.room_type)
            # self.scene_update_audio_stream()

            # 步骤6: 更新视频流
            format_room_log("步骤6: 更新视频流", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                            user_id=self.userUuid, room_type=self.room_type)
            self.scene_update_video_stream()

            format_room_log("完成加入房间流程", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                            user_id=self.userUuid, room_type=self.room_type)
            time.sleep(CONFIG.get("room_keep_seconds"))

            # 步骤7: 下线
            # format_room_log("步骤7: 下线", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
            #                 user_id=self.userUuid, room_type=self.room_type)
            # self.scene_offline()
        except Exception as e:
            format_room_log(f"加入房间流程异常: {str(e)}", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                            user_id=self.userUuid)
            logger.error(f"异常详情: {str(e)}")
            import traceback
            logger.error(f"异常堆栈: {traceback.format_exc()}")
        finally:
            format_room_log(f"任务结束", room_id=self.virtual_room_id, real_room_id=self.roomUuid,user_id=self.userUuid)
            # 完成用户生命周期
            self._finish_user()
            self.stop(force=True)

    def create_scene_room(self):
        try:
            payload = {
                "roomName": f"房间{self.virtual_room_id}",
                "roomProperties": {},
                "roomTemplate": "conf_finity_v1"
            }

            server_token = room_pool.get_server_token()
            # 设置请求headers
            headers = {
                'Authorization': f'agora token="{server_token}"',
                'X-Agora_token': server_token,
                'X-Agora-Uid': self.userUuid
            }

            # 构建进入房间的URL
            format_room_log("步骤1: 创建房间", room_id=self.virtual_room_id, user_id=self.userUuid)
            self.client.request_name = "/cn/conference/apps/[appId]/v1/rooms/[roomUuid]"
            with self.client.post(
                    f"/cn/conference/apps/{self.app_id}/v1/rooms/{self.virtual_room_id}",
                    headers=headers,
                    json=payload,
                    catch_response=True
            ) as response:
                if response.status_code == 409:
                    response.success()
                    return
                if response.status_code == 200:
                    result = response.json()
                    if result.get("code") == 0:
                        response.success()
                        room_pool.set_real_room_id(self.virtual_room_id, self.virtual_room_id, self.userUuid)
                        self.roomUuid = self.virtual_room_id
                    else:
                        response.failure(f"创建房间失败: {result}")
                        format_room_log(f"创建房间失败: {result.get('message')}", user_id=self.userUuid,
                                        room_type=self.room_type, virtual_room_id=self.virtual_room_id)
                else:
                    response.failure(f"HTTP错误: {response.status_code}")
                    format_room_log(f"创建房间HTTP错误: {response.status_code}", user_id=self.userUuid,
                                    room_type=self.room_type, virtual_room_id=self.virtual_room_id)
        except Exception as e:
            format_room_log(f"创建房间异常: {str(e)}", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                            user_id=self.userUuid)

    def start_scene_room(self, server_token):
        """开始会议 - 设置房间state=1"""
        if self.roomUuid and self.app_id:
            try:
                format_room_log("步骤1.1: 开始会议 - 设置房间state=1", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid)
                headers = {
                    'Authorization': f'agora token="{server_token}"',
                    'X-Agora_token': server_token,
                    'X-Agora-Uid': self.userUuid
                }

                # 构建开始会议的URL，state=1表示开始会议
                start_meeting_url = f"/cn/conference/apps/{self.app_id}/v1/rooms/{self.roomUuid}/states/1"
                self.client.request_name = "/cn/conference/apps/[appId]/v1/rooms/[roomUuid]/states/1"
                with self.client.put(
                        start_meeting_url,
                        headers=headers,
                        catch_response=True
                ) as response:
                    if response.status_code == 200:
                        result = response.json()
                        if result.get("code") == 0:
                            response.success()
                            format_room_log("开始会议成功", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                            user_id=self.userUuid)
                        else:
                            response.failure(f"开始会议失败: {result.get('message')}")
                            format_room_log(f"开始会议失败: {result.get('message')}", room_id=self.virtual_room_id,
                                            real_room_id=self.roomUuid, user_id=self.userUuid)
                    else:
                        response.failure(f"HTTP错误: {response.status_code}")
                        format_room_log(f"开始会议HTTP错误: {response.status_code}", room_id=self.virtual_room_id,
                                        real_room_id=self.roomUuid, user_id=self.userUuid)
            except Exception as e:
                format_room_log(f"开始会议异常: {str(e)}", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid)

    def get_scene_token(self):
        """获取token"""
        if self.roomUuid and self.app_id and self.userUuid:
            try:
                format_room_log("开始获取token", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid)
                self.client.request_name = "/conference/v3/rooms/[roomUuid]/roles/[role]/users/[userUuid]/token"
                with self.client.get(
                        f"/conference/v3/rooms/{self.roomUuid}/roles/3/users/{self.userUuid}/token",
                        catch_response=True
                ) as response:
                    if response.status_code == 200:
                        result = response.json()
                        if result.get("code") == 0:
                            # 从响应中获取streamUuid
                            self.token = result.get("data", {}).get("token")
                            response.success()
                            format_room_log("获取token成功", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                            user_id=self.userUuid)
                        else:
                            response.failure(f"获取token失败: {result.get('message')}")
                            format_room_log(f"获取token失败: {result.get('message')}", room_id=self.virtual_room_id,
                                            real_room_id=self.roomUuid, user_id=self.userUuid)
                    else:
                        response.failure(f"HTTP错误: {response.status_code}")
                        format_room_log(f"获取tokenHTTP错误: {response.status_code}", room_id=self.virtual_room_id,
                                        real_room_id=self.roomUuid, user_id=self.userUuid)
            except Exception as e:
                format_room_log(f"获取token异常: {str(e)}", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid)


    def entry_scene_room(self):
        """进入房间"""
        if self.roomUuid and self.app_id and self.userUuid and self.token:

            try:
                payload = {
                    "password": "",
                    "platform": 1,
                    "role": "participant",
                    "streams": [
                        {
                            "streamName": "default",
                            "audioState": 1,
                            "videoState": 1,
                            "videoSourceType": 1,
                            "audioSourceType": 1
                        }
                    ],
                    "userName": f"用户{self.userUuid}",
                    "version": "3.7.0"
                }

                # 设置请求headers
                headers = {
                    'Authorization': f'agora token="{self.token}"',
                    'X-Agora_token': self.token,
                    'X-Agora-Uid': self.userUuid
                }

                # 构建进入房间的URL
                entry_url = f"/cn/conference/apps/{self.app_id}/v1/rooms/{self.roomUuid}/users/{self.userUuid}/entry"

                format_room_log("开始进入房间", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid)
                self.client.request_name = "/cn/conference/apps/[appId]/v1/rooms/[roomUuid]/users/[userUuid]/entry"
                with self.client.put(
                        entry_url,
                        json=payload,
                        headers=headers,
                        catch_response=True
                ) as response:
                    if response.status_code == 200:
                        result = response.json()
                        if result.get("code") == 0:
                            # 从响应中获取streamUuid
                            self.streamUuid = result.get("data", {}).get("localUser", {}).get("streamUuid")
                            response.success()
                            format_room_log("进入房间成功", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                            user_id=self.userUuid, stream_uuid=self.streamUuid)
                        else:
                            response.failure(f"进入房间失败: {result.get('message')}")
                            format_room_log(f"进入房间失败: {result.get('message')}", room_id=self.virtual_room_id,
                                            real_room_id=self.roomUuid, user_id=self.userUuid)
                    else:
                        response.failure(f"HTTP错误: {response.status_code}")
                        format_room_log(f"进入房间HTTP错误: {response.status_code}", room_id=self.virtual_room_id,
                                        real_room_id=self.roomUuid, user_id=self.userUuid)
            except Exception as e:
                format_room_log(f"进入房间异常: {str(e)}", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid)


    def scene_online(self):
        """上线状态"""
        if self.roomUuid and self.app_id and self.userUuid and self.token:
            try:
                # 设置请求headers
                headers = {
                    'Authorization': f'agora token="{self.token}"',
                    'X-Agora_token': self.token,
                    'X-Agora-Uid': self.userUuid
                }

                # 构建上线URL，state=1代表上线
                online_url = f"/cn/scene/apps/{self.app_id}/v1/rooms/{self.roomUuid}/users/{self.userUuid}/states/1"
                format_room_log("开始上线", room_id=self.virtual_room_id, real_room_id=self.roomUuid,user_id=self.userUuid)
                self.client.request_name = "/cn/scene/apps/[appId]/v1/rooms/[roomUuid]/users/[userUuid]/states/1"
                with self.client.put(
                        online_url,
                        headers=headers,
                        catch_response=True
                ) as response:
                    if response.status_code == 200:
                        result = response.json()
                        if result.get("code") == 0:
                            response.success()
                            format_room_log("上线成功", room_id=self.virtual_room_id, real_room_id=self.roomUuid,user_id=self.userUuid)
                            
                            # 用户成功上线后，记录在线状态并检查是否达到房间容量
                            is_room_full = room_pool.user_online(self.virtual_room_id, self.userUuid)
                            if is_room_full:
                                format_room_log("房间已满员", room_id=self.virtual_room_id, real_room_id=self.roomUuid, 
                                               user_id=self.userUuid, room_type=self.room_type)
                        else:
                            response.failure(f"上线失败: {result.get('message')}")
                            format_room_log(f"上线失败: {result.get('message')}", room_id=self.virtual_room_id,
                                            real_room_id=self.roomUuid, user_id=self.userUuid)
                    else:
                        response.failure(f"HTTP错误: {response.status_code}")
                        format_room_log(f"上线HTTP错误: {response.status_code}", room_id=self.virtual_room_id,
                                        real_room_id=self.roomUuid, user_id=self.userUuid)
            except Exception as e:
                format_room_log(f"上线异常: {str(e)}", room_id=self.virtual_room_id, real_room_id=self.roomUuid, user_id=self.userUuid)


    def scene_offline(self):
        """下线状态"""
        if self.roomUuid and self.app_id and self.userUuid and self.token:
            try:
                # 设置请求headers
                headers = {
                    'Authorization': f'agora token="{self.token}"',
                    'X-Agora_token': self.token,
                    'X-Agora-Uid': self.userUuid
                }

                # 构建下线URL，state=0代表下线
                offline_url = f"/cn/scene/apps/{self.app_id}/v1/rooms/{self.roomUuid}/users/{self.userUuid}/states/0"
                format_room_log("开始下线", room_id=self.virtual_room_id, real_room_id=self.roomUuid,user_id=self.userUuid)
                self.client.request_name = "/cn/scene/apps/[appId]/v1/rooms/[roomUuid]/users/[userUuid]/states/0"
                with self.client.put(
                        offline_url,
                        headers=headers,
                        catch_response=True
                ) as response:
                    if response.status_code == 200:
                        result = response.json()
                        if result.get("code") == 0:
                            response.success()
                            format_room_log("下线成功", room_id=self.virtual_room_id, real_room_id=self.roomUuid,user_id=self.userUuid)
                        else:
                            response.failure(f"下线失败: {result.get('message')}")
                            format_room_log(f"下线失败: {result.get('message')}", room_id=self.virtual_room_id,real_room_id=self.roomUuid, user_id=self.userUuid)
                    else:
                        response.failure(f"HTTP错误: {response.status_code}")
                        format_room_log(f"下线HTTP错误: {response.status_code}", room_id=self.virtual_room_id, real_room_id=self.roomUuid, user_id=self.userUuid)
            except Exception as e:
                format_room_log(f"更新视频流异常: {str(e)}", room_id=self.virtual_room_id, real_room_id=self.roomUuid,user_id=self.userUuid)

    def scene_update_audio_stream(self):
        """更新音频流"""
        if self.roomUuid and self.app_id and self.token and self.streamUuid:
            try:
                payload = {
                    "streams": [
                        {
                            "streamUuid": self.streamUuid,
                            "audioSourceUuid": "default"
                        }
                    ]
                }

                # 设置请求headers
                headers = {
                    'Authorization': f'agora token="{self.token}"',
                    'X-Agora_token': self.token,
                    'X-Agora-Uid': self.userUuid
                }

                # 构建更新音频流的URL
                update_audio_url = f"/cn/conference/apps/{self.app_id}/v1/rooms/{self.roomUuid}/streams"

                format_room_log("开始更新音频流", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid, stream_uuid=self.streamUuid)
                self.client.request_name = "/cn/conference/apps/[appId]/v1/rooms/[roomUuid]/streams"
                with self.client.put(
                        update_audio_url,
                        json=payload,
                        headers=headers,
                        catch_response=True
                ) as response:
                    if response.status_code == 200:
                        result = response.json()
                        if result.get("code") == 0:
                            response.success()
                            format_room_log("更新音频流成功", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                            user_id=self.userUuid, stream_uuid=self.streamUuid)
                        else:
                            response.failure(f"更新音频流失败: {result.get('message')}")
                            format_room_log(f"更新音频流失败: {result.get('message')}", room_id=self.virtual_room_id,
                                            real_room_id=self.roomUuid, user_id=self.userUuid)
                    else:
                        response.failure(f"HTTP错误: {response.status_code}")
                        format_room_log(f"更新音频流HTTP错误: {response.status_code}", room_id=self.virtual_room_id,
                                        real_room_id=self.roomUuid, user_id=self.userUuid)
            except Exception as e:
                format_room_log(f"更新音频流异常: {str(e)}", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid)


    def scene_update_video_stream(self):
        """更新视频流"""
        if self.roomUuid and self.app_id and self.token and self.streamUuid:
            try:
                payload = {
                    "streams": [
                        {"audioSourceState": 0, "audioSourceUuid": "1", "audioState": 1, "videoSourceState": 0,
                         "videoSourceUuid": "1", "videoState": 1, "streamUuid": self.streamUuid,}
                    ]
                }

                # 设置请求headers
                headers = {
                    'Authorization': f'agora token="{self.token}"',
                    'X-Agora_token': self.token,
                    'X-Agora-Uid': self.userUuid
                }

                # 构建更新视频流的URL
                update_video_url = f"/cn/conference/apps/{self.app_id}/v1/rooms/{self.roomUuid}/streams"

                format_room_log("开始更新视频流", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid, stream_uuid=self.streamUuid)
                self.client.request_name = "/cn/conference/apps/[appId]/v1/rooms/[roomUuid]/streams"
                with self.client.put(
                        update_video_url,
                        json=payload,
                        headers=headers,
                        catch_response=True
                ) as response:
                    if response.status_code == 200:
                        result = response.json()
                        if result.get("code") == 0:
                            response.success()
                            format_room_log("更新视频流成功", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                            user_id=self.userUuid, stream_uuid=self.streamUuid)
                        else:
                            response.failure(f"更新视频流失败: {result.get('message')}")
                            format_room_log(f"更新视频流失败: {result.get('message')}", room_id=self.virtual_room_id,
                                            real_room_id=self.roomUuid, user_id=self.userUuid)
                    else:
                        response.failure(f"HTTP错误: {response.status_code}")
                        format_room_log(f"更新视频流HTTP错误: {response.status_code}", room_id=self.virtual_room_id,
                                        real_room_id=self.roomUuid, user_id=self.userUuid)
            except Exception as e:
                format_room_log(f"更新视频流异常: {str(e)}", room_id=self.virtual_room_id, real_room_id=self.roomUuid,
                                user_id=self.userUuid)

    def _finish_user(self):
        """完成用户生命周期，从房间池中移除用户"""
        # 防止重复调用
        if hasattr(self, '_finished'):
            logger.warning(f"用户 {self.userUuid} 已经完成过，跳过")
            return
        self._finished = True

        # 从房间池中移除用户
        if self.virtual_room_id:
            format_room_log("开始从房间池中移除用户", room_id=self.virtual_room_id, user_id=self.userUuid)
            room_pool.leave_virtual_room(self.virtual_room_id, self.userUuid)
            format_room_log("已从房间池中移除用户", room_id=self.virtual_room_id, user_id=self.userUuid)
        else:
            logger.warning(f"用户 {self.userUuid} 没有virtual_room_id，跳过房间池移除")

        # 更新用户完成计数
        global completed_users
        completed_users += 1
        # print(f"用户完成: {completed_users}/{total_users} - 用户ID: {self.userUuid}")

        # 输出房间统计信息
        stats = room_pool.get_room_stats()
        # print(f"房间统计: 总用户数={stats['total_users']}, 已离开用户数={stats['left_users']}")

@events.test_start.add_listener
def on_test_start(environment, **kwargs):
    # 测试开始时启动一个后台协程，监听用户退出
    def monitor_users():
        while environment.runner.user_count > 0:
            time.sleep(5)
        logger.info("\n===== 所有用户任务已完成 =====")

    gevent.spawn(monitor_users)

# 注册 test_stop 事件回调
@events.test_stop.add_listener
def on_test_stop(environment, **kwargs):
    logger.info("\n===== 所有用户已退出 =====")
    logger.info(f"总请求数: {environment.stats.total.num_requests}")
    logger.info(f"失败请求数: {environment.stats.total.num_failures}")
    logger.info("===== 测试结束 =====\n")
