
class Engine(object):
    """
    Data Engine class

    To add support for a specific backend simply implement this Engine class
    """

    @classmethod
    def from_conf(cls, conf):
        """
        Construct a data Engine from a config object
        """
        pass

    def initialize(self):
        """
        Initialize the database engine

        Called at each startup
        """
        pass

    def get_reader(self):
        """
        Return reader instance for this engine
        """
        pass

    def get_writer(self):
        """
        Return writer instance for this engine
        """
        pass
