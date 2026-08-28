import unittest
from unittest.mock import MagicMock, patch

from agentfleet.client import FleetClient
from agentfleet.models import InstanceInfo, SwarmTeamInfo, TaskInfo
from agentfleet.bot import Bot
from agentfleet.task import Task
from agentfleet.swarm import Swarm


class TestAgentFleetSDK(unittest.TestCase):
    def setUp(self):
        self.client = FleetClient("http://mock-fleet:8080", token="mock-token")

    def test_instance_model_parsing(self):
        data = {
            "id": "inst-cyber-01",
            "name": "RedTeam-01",
            "tier": "standard",
            "state": "running",
            "archetype_id": "cyber_ops",
            "shell_access": True,
            "preinstalled_tools": ["nmap", "wireshark"],
        }
        info = InstanceInfo.from_dict(data)
        self.assertEqual(info.id, "inst-cyber-01")
        self.assertEqual(info.archetype_id, "cyber_ops")
        self.assertTrue(info.shell_access)
        self.assertEqual(len(info.preinstalled_tools), 2)

    def test_task_model_parsing(self):
        data = {
            "id": "task-101",
            "instance_id": "inst-cyber-01",
            "goal": "Scan network",
            "state": "succeeded",
            "step": 4,
            "max_steps": 60,
            "result": "Port 80 open",
        }
        info = TaskInfo.from_dict(data)
        self.assertEqual(info.id, "task-101")
        self.assertEqual(info.state, "succeeded")
        self.assertEqual(info.result, "Port 80 open")

    def test_swarm_model_parsing(self):
        data = {
            "id": "swarm-99",
            "name": "Audit Swarm",
            "mission": "Pen test login flow",
            "status": "running",
            "members": [
                {
                    "instance_id": "inst-1",
                    "instance_name": "FullStack Bot",
                    "role": "Lead Architect",
                    "archetype_id": "fullstack_dev",
                    "status": "working",
                }
            ],
            "messages": [
                {
                    "id": "msg-1",
                    "swarm_id": "swarm-99",
                    "from_bot": "FullStack Bot",
                    "to_bot": "all",
                    "phase": "planning",
                    "content": "Architecture plan approved.",
                    "created_at": "2026-08-28T12:00:00Z",
                }
            ],
        }
        info = SwarmTeamInfo.from_dict(data)
        self.assertEqual(info.id, "swarm-99")
        self.assertEqual(len(info.members), 1)
        self.assertEqual(len(info.messages), 1)
        self.assertEqual(info.messages[0].phase, "planning")

    @patch.object(FleetClient, "_post")
    def test_deploy_bot(self, mock_post):
        mock_post.return_value = {
            "id": "inst-123",
            "name": "nightly-auditor",
            "tier": "standard",
            "state": "running",
            "archetype_id": "cyber_ops",
        }

        bot = self.client.deploy_bot("cyber_ops", name="nightly-auditor")
        self.assertEqual(bot.id, "inst-123")
        self.assertEqual(bot.name, "nightly-auditor")
        self.assertEqual(bot.archetype_id, "cyber_ops")
        mock_post.assert_called_once()

    @patch.object(FleetClient, "_post")
    def test_run_task(self, mock_post):
        mock_post.return_value = {
            "id": "task-555",
            "instance_id": "inst-123",
            "goal": "Test goal",
            "state": "pending",
        }
        bot_info = InstanceInfo(id="inst-123", name="test-bot", tier="standard", state="running")
        bot = Bot(self.client, bot_info)
        task = bot.run("Test goal", wait=False)
        self.assertEqual(task.id, "task-555")
        self.assertEqual(task.goal, "Test goal")

    @patch.object(FleetClient, "_post")
    def test_launch_swarm(self, mock_post):
        mock_post.return_value = {
            "id": "swarm-001",
            "name": "App Security Team",
            "mission": "Run penetration test and accessibility verification",
            "status": "running",
            "members": [],
            "messages": [],
        }
        swarm = self.client.launch_swarm("App Security Team", "Run penetration test and accessibility verification")
        self.assertEqual(swarm.id, "swarm-001")
        self.assertEqual(swarm.name, "App Security Team")


if __name__ == "__main__":
    unittest.main()
