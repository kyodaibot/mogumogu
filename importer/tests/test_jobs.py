import unittest
from datetime import datetime, timedelta
from jobs import JST, due_phase, retry_delay
class JobTests(unittest.TestCase):
    def test_boundaries_and_overlap(self):
        morning = datetime(2026,11,23,8,tzinfo=JST)
        due = morning + timedelta(minutes=10)
        self.assertIsNone(due_phase(morning-timedelta(seconds=1),{}))
        self.assertEqual(due_phase(morning,{}), "morning")
        self.assertIsNone(due_phase(morning,{"morning":due}))
        self.assertEqual(due_phase(due,{"morning":due}), "morning")
        self.assertIsNone(due_phase(due,{"morning":None}))
        noon = morning + timedelta(hours=2,minutes=30)
        self.assertEqual(due_phase(noon,{"morning":noon+timedelta(hours=1)}), "review")
        self.assertIsNone(due_phase(noon,{"review":None}))
        self.assertEqual([retry_delay(i).total_seconds()//60 for i in [1,2,3,10]], [10,30,60,60])
