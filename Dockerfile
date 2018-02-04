FROM ubuntu

WORKDIR /picopublish

ADD * /picopublish/
ADD static/* /picopublish/static/

RUN chmod +x /picopublish/picopublish

ENTRYPOINT /picopublish/picopublish
